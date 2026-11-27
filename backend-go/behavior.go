package shanhecaptcha

import "math"

// 前端传来的拖动信号。字段可以伪造，只用来辅助判断。
// Events 是事件数，Trusted 是真实事件数，Coalesced 是合并后的采样数，Keyboard 是键盘操作。
type BehaviorSignals struct {
	PointerType string `json:"pointerType,omitempty"`
	Events      int    `json:"events,omitempty"`
	Trusted     int    `json:"trusted,omitempty"`
	Coalesced   int    `json:"coalesced,omitempty"`
	Keyboard    bool   `json:"keyboard,omitempty"`
}

// 行为评分配置。分数范围 0-100，低于 MinScore 拒绝。
type BehaviorConfig struct {
	Enabled  bool
	MinScore int
	// 键盘操作默认不评分；改成 true 后也参与评分。
	StrictKeyboard bool
}

// 计算一次拖动的分数。
func (c *Captcha) scoreBehavior(trail []TrailPoint, sig *BehaviorSignals) (int, []string) {
	if !c.cfg.Behavior.Enabled {
		return 100, nil
	}
	if sig != nil && sig.Keyboard && !c.cfg.Behavior.StrictKeyboard {
		// 键盘没有指针轨迹，默认跳过评分。
		return 100, []string{"keyboard_bypass"}
	}

	score := 100.0
	untrusted := false
	notes := make([]string, 0, 8)
	penalty := func(v float64, why string) {
		score -= v
		notes = append(notes, why)
	}
	bonus := func(v float64, why string) {
		score += v
		notes = append(notes, "+"+why)
	}

	n := len(trail)
	if n < 3 {
		return 0, []string{"too_few_points"}
	}
	first, last := trail[0], trail[n-1]
	dur := float64(last.T - first.T)
	if dur <= 0 {
		return 0, []string{"zero_duration"}
	}

	var sumSpeed, sumSpeed2 float64
	var maxSpeed float64
	peakIdx := 0
	var prevSign int
	accelTurns := 0
	var sumDT, sumDT2 float64
	pauses := 0
	for i := 1; i < n; i++ {
		dt := float64(trail[i].T - trail[i-1].T)
		if dt <= 0 {
			dt = 1
		}
		d := math.Hypot(trail[i].X-trail[i-1].X, trail[i].Y-trail[i-1].Y)
		v := d / dt
		sumSpeed += v
		sumSpeed2 += v * v
		if v > maxSpeed {
			maxSpeed = v
			peakIdx = i
		}
		if dt > 120 {
			pauses++
		}
		sumDT += dt
		sumDT2 += dt * dt

		if i >= 3 {
			dv1 := math.Hypot(trail[i-1].X-trail[i-2].X, trail[i-1].Y-trail[i-2].Y) / float64(max64(1, trail[i-1].T-trail[i-2].T))
			dv2 := v
			sign := 0
			if dv2 > dv1*1.15 {
				sign = 1
			} else if dv2 < dv1*0.85 {
				sign = -1
			}
			if sign != 0 && prevSign != 0 && sign != prevSign {
				accelTurns++
			}
			if sign != 0 {
				prevSign = sign
			}
		}
	}
	meanSpeed := sumSpeed / float64(n-1)
	varSpeed := sumSpeed2/float64(n-1) - meanSpeed*meanSpeed
	speedCV := 0.0
	if meanSpeed > 0 && varSpeed > 0 {
		speedCV = math.Sqrt(varSpeed) / meanSpeed
	}

	meanDT := sumDT / float64(n-1)
	varDT := sumDT2/float64(n-1) - meanDT*meanDT
	dtCV := 0.0
	if meanDT > 0 && varDT > 0 {
		dtCV = math.Sqrt(varDT) / meanDT
	}

	var sumY, sumY2 float64
	deviation := 0.0
	lineDX := last.X - first.X
	for _, p := range trail {
		sumY += p.Y
		sumY2 += p.Y * p.Y
		if lineDX != 0 {
			expected := first.Y + (p.X-first.X)/lineDX*(last.Y-first.Y)
			if d := math.Abs(p.Y - expected); d > deviation {
				deviation = d
			}
		}
	}
	meanY := sumY / float64(n)
	varY := sumY2/float64(n) - meanY*meanY
	yStd := 0.0
	if varY > 0 {
		yStd = math.Sqrt(varY)
	}

	maxX := first.X
	for _, p := range trail {
		if p.X > maxX {
			maxX = p.X
		}
	}
	overshoot := maxX - last.X

	peakRatio := float64(peakIdx) / float64(n-1)

	if sig == nil {
		penalty(35, "missing_signals")
	} else {
		if sig.Events == 0 {
			penalty(25, "no_pointer_events")
		}
		if sig.Events > 0 && sig.Trusted < sig.Events {
			penalty(30, "untrusted_events")
			untrusted = true
		}
		if sig.PointerType == "" {
			penalty(8, "no_pointer_type")
		}
		if sig.Coalesced == 0 && sig.PointerType == "mouse" {
			penalty(4, "no_coalesced_events")
		}
		if sig.PointerType == "touch" || sig.PointerType == "pen" {
			bonus(2, "touch_or_pen")
		}
		if sig.Events >= 10 && sig.Trusted == sig.Events {
			bonus(6, "trusted_events")
		}
		if sig.Coalesced > sig.Events {
			bonus(3, "high_frequency_sampling")
		}
	}

	if n < 12 {
		penalty(12, "few_samples")
	}
	if dur < 200 {
		penalty(15, "too_fast")
	}
	if dur > 8000 {
		penalty(10, "too_slow")
	}
	if dtCV < 0.06 {
		penalty(15, "uniform_timing")
	}
	if speedCV < 0.10 {
		penalty(18, "constant_speed")
	}
	if accelTurns == 0 {
		penalty(8, "no_acceleration_turns")
	}
	if yStd < 0.08 {
		penalty(10, "perfectly_flat")
	}
	if pauses == 0 && dur > 500 {
		penalty(6, "no_pauses")
	}
	if deviation < 0.05 && yStd < 0.2 {
		penalty(5, "perfectly_straight")
	}
	if overshoot <= 0 && dur < 700 {
		penalty(4, "no_overshoot")
	}
	if peakRatio < 0.05 || peakRatio > 0.95 {
		penalty(5, "unnatural_peak")
	}

	if yStd > 0.3 && yStd < 8 {
		bonus(4, "vertical_jitter")
	}
	if overshoot > 0.5 && overshoot < 15 {
		bonus(6, "human_overshoot")
	}
	if speedCV > 0.25 {
		bonus(5, "speed_variation")
	}
	if pauses >= 1 {
		bonus(4, "has_pause")
	}

	if untrusted && score > 35 {
		score = 35
		notes = append(notes, "untrusted_cap")
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return int(math.Round(score)), notes
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
