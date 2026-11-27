package shanhecaptcha

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"time"
)

func (c *Captcha) verify(ctx context.Context, r *http.Request, req *VerifyRequest) *VerifyResult {
	if req == nil || req.ID == "" {
		return &VerifyResult{OK: false, Code: CodeBadRequest}
	}

	key := challengeKeyPrefix + req.ID

	raw, err := c.take(ctx, key)
	if err != nil {
		return &VerifyResult{OK: false, Code: CodeInvalid}
	}
	var rec challengeRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return &VerifyResult{OK: false, Code: CodeInvalid}
	}

	if c.cfg.BindToClient && rec.ClientKey != c.ClientKey(r) {
		c.emit(Event{Kind: "failure", ChallengeID: rec.ID, ClientKey: rec.ClientKey, Reason: CodeClientMismatch, At: c.now()})
		return &VerifyResult{OK: false, Code: CodeClientMismatch}
	}

	now := c.now()
	elapsed := now.Sub(rec.CreatedAt)
	if elapsed > c.cfg.ChallengeTTL {
		return &VerifyResult{OK: false, Code: CodeExpired}
	}
	if elapsed < c.cfg.MinSolveDuration {
		c.penalize(ctx, key, &rec)
		c.emit(Event{Kind: "failure", ChallengeID: rec.ID, ClientKey: rec.ClientKey, Reason: CodeTooFast, Elapsed: elapsed, At: now})
		return &VerifyResult{OK: false, Code: CodeTooFast}
	}

	if c.cfg.PoW.Enabled && rec.Stage != "plain" {
		if rec.PoWPrefix == "" || !verifyPoW(rec.PoWPrefix, c.cfg.PoW.Bits, req.Nonce) {
			c.penalize(ctx, key, &rec)
			return &VerifyResult{OK: false, Code: CodePoW}
		}
	}

	if c.cfg.VerifyTrail {
		if !c.checkTrail(req.Trail, req.X) {
			c.penalize(ctx, key, &rec)
			c.emit(Event{Kind: "failure", ChallengeID: rec.ID, ClientKey: rec.ClientKey, Reason: CodeTrail, Elapsed: elapsed, At: now})
			return &VerifyResult{OK: false, Code: CodeTrail}
		}
	}

	if math.Abs(req.X-rec.AnswerX) > float64(c.cfg.Tolerance) {
		c.penalize(ctx, key, &rec)
		c.emit(Event{
			Kind:        "failure",
			ChallengeID: rec.ID,
			ClientKey:   rec.ClientKey,
			Reason:      CodeMismatch,
			Elapsed:     elapsed,
			At:          now,
		})
		return &VerifyResult{OK: false, Code: CodeMismatch}
	}

	score := 100
	if c.cfg.Behavior.Enabled {
		var notes []string
		score, notes = c.scoreBehavior(req.Trail, req.Behavior)
		if score < c.cfg.Behavior.MinScore {
			c.penalize(ctx, key, &rec)
			c.emit(Event{Kind: "failure", ChallengeID: rec.ID, ClientKey: rec.ClientKey, Reason: CodeBehavior, Elapsed: elapsed, At: now})
			_ = notes
			return &VerifyResult{OK: false, Code: CodeBehavior, Score: score}
		}
	}

	if c.cfg.SecondGate.Enabled && rec.Stage != "plain" {
		plainID, err := c.newPlainChallenge(ctx, &rec)
		if err != nil {
			c.cfg.Logger.Error("shanhecaptcha: create second gate failed", "err", err)
			return &VerifyResult{OK: false, Code: CodeInvalid, Score: score}
		}
		return &VerifyResult{OK: true, Next: "plain", NextID: plainID, Score: score, Retry: false}
	}

	ticket, err := c.issueTicket(ctx, &rec)
	if err != nil {
		c.cfg.Logger.Error("shanhecaptcha: issue ticket failed", "err", err)
		return &VerifyResult{OK: false, Code: CodeInvalid}
	}

	c.emit(Event{Kind: "success", ChallengeID: rec.ID, ClientKey: rec.ClientKey, Elapsed: elapsed, At: now})
	return &VerifyResult{OK: true, Ticket: ticket, Retry: false, Score: score}
}

func (c *Captcha) penalize(ctx context.Context, key string, rec *challengeRecord) {
	rec.AttemptsLeft--
	if rec.AttemptsLeft > 0 {
		remaining := rec.CreatedAt.Add(c.cfg.ChallengeTTL).Sub(c.now())
		if remaining > 0 {
			if err := c.put(ctx, key, rec, remaining); err == nil {
				return
			}
		}
	}
	_ = c.store.Delete(ctx, key)
}

func (c *Captcha) checkTrail(trail []TrailPoint, finalX float64) bool {
	if len(trail) < c.cfg.MinTrailPoints || len(trail) > c.cfg.MaxTrailPoints {
		return false
	}
	if trail[0].T < 0 {
		return false
	}
	if math.Abs(trail[0].X) > 6 || math.Abs(trail[0].Y) > 6 {
		return false
	}
	prev := trail[0]
	for _, p := range trail[1:] {
		dt := float64(p.T - prev.T)
		if dt < 0 {
			return false
		}
		if dt < 1 {
			dt = 1
		}
		d := math.Hypot(p.X-prev.X, p.Y-prev.Y)
		if d/dt > c.cfg.MaxSpeedPxPerMs {
			return false
		}
		prev = p
	}
	last := trail[len(trail)-1]
	if math.Abs(last.X-finalX) > 3 {
		return false
	}
	if math.Abs(last.X) < 1 {
		return false
	}
	if last.T > int64(c.cfg.ChallengeTTL/time.Millisecond) {
		return false
	}
	return true
}
