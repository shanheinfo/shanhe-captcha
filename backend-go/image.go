package shanhecaptcha

import (
	"context"
	"image"
	"image/color"
	"math"
	"math/rand"
	"time"
)

// GenRequest 是一次图片生成请求。
type GenRequest struct {
	Width     int
	Height    int
	PieceSize int
	// Decoys 是假缺口数量，最多 3 个。第一个优先放在真缺口同一行，其余随机放。
	// 自定义生成器可以忽略这个字段。
	Decoys int
	// 随机源。相同的 seed 可以复现同一张图。
	Rand *rand.Rand
}

// 生成结果。背景图上已经画好缺口，碎片带透明通道。
type GenResult struct {
	Background *image.NRGBA
	Piece      *image.NRGBA
	// 碎片落点的左上角坐标，只保存在服务端。
	TargetX int
	TargetY int
	// 碎片一开始显示的 x 坐标。
	PieceX int
	PieceW int
	PieceH int
	// 实际画上去的假缺口数量。
	Decoys int
}

// Generator 是图片生成接口。
// 自定义实现要保证背景已经画好缺口，碎片能拼回目标位置。
type Generator interface {
	Generate(ctx context.Context, req GenRequest) (*GenResult, error)
}

// DefaultGenerator 是内置生成器，每次画一张新图。
type DefaultGenerator struct{}

// Generate 生成一张挑战图。
func (DefaultGenerator) Generate(_ context.Context, req GenRequest) (*GenResult, error) {
	rnd := req.Rand
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	w, h := req.Width, req.Height
	if w < 200 {
		w = 200
	}
	if h < 100 {
		h = 100
	}
	ps := req.PieceSize
	if ps <= 0 || ps > h-16 {
		ps = h - 16
	}

	bg := paintBackground(rnd, w, h)
	pristine := cloneNRGBA(bg)

	pw := ps + rnd.Intn(13) - 6
	if pw < 24 {
		pw = 24
	}
	if pw > w-100 {
		pw = w - 100
	}
	ph := pw
	if ph > h-24 {
		ph = h - 24
		pw = ph
	}

	decoys := req.Decoys
	if decoys < 0 {
		decoys = 0
	}
	if decoys > 3 {
		decoys = 3
	}

	pieceX := 10 + rnd.Intn(8)
	gap, sameRowSide := 0, -1
	if decoys > 0 {
		gap = 18 + rnd.Intn(7)
		sameRowSide = rnd.Intn(2)
	}
	targetX := 0
	if sameRowSide >= 0 {
		var ok bool
		targetX, ok = sameRowTargetX(rnd, w, pw, pieceX, gap, sameRowSide)
		if !ok {
			sameRowSide = 1 - sameRowSide
			targetX, ok = sameRowTargetX(rnd, w, pw, pieceX, gap, sameRowSide)
		}
		if !ok {
			sameRowSide = -1
		}
	}
	if sameRowSide < 0 {
		targetX = randomTargetX(rnd, pieceX+60+pw, w-pw-12)
	}

	minY := 12
	maxY := h - ph - 12
	if maxY < minY {
		maxY = minY
	}
	targetY := minY
	if maxY-minY > 0 {
		targetY += rnd.Intn(maxY - minY + 1)
	}

	maskN := 5 + rnd.Intn(4)
	maskPts := polygonPoints(pw, ph, rnd, maskN)
	mask := rasterPolygon(pw, ph, maskPts)
	piece := cutPiece(pristine, mask, targetX, targetY, pw, ph)

	// 真缺口和假缺口用同一套质感，避免靠画法认出真缺口。
	style := newHoleStyle(rnd)
	sampleX, sampleY := distantSample(rnd, w, h, targetX, targetY, pw, ph)
	drawHoleFrom(bg, mask, targetX, targetY, pw, ph, style, rnd, pristine, sampleX, sampleY)
	trueRect := image.Rect(targetX, targetY, targetX+pw, targetY+ph)
	pieceRect := image.Rect(pieceX, targetY, pieceX+pw, targetY+ph)
	holes := []image.Rectangle{trueRect}
	actualDecoys := 0
	remaining := decoys

	if remaining > 0 && sameRowSide >= 0 {
		if dx, ok := sameRowDecoyX(w, targetX, targetY, pw, ph, pieceX, gap, sameRowSide); ok {
			decoyMask := rasterPolygon(pw, ph, perturbPolygon(maskPts, pw, ph, rnd))
			drawHole(bg, decoyMask, dx, targetY, pw, ph, style, rnd, pristine)
			holes = append(holes, image.Rect(dx, targetY, dx+pw, targetY+ph))
			actualDecoys++
			remaining--
		}
	}

	for i := 0; i < remaining; i++ {
		var dx, dy int
		ok := false
		for tries := 0; tries < 48; tries++ {
			dx = 12 + rnd.Intn(max(1, w-pw-24))
			dy = 12 + rnd.Intn(max(1, h-ph-24))
			r := image.Rect(dx, dy, dx+pw, dy+ph)
			if tries < 32 && !(r.Min.Y < targetY-8 || r.Max.Y > targetY+ph+8) {
				continue
			}
			inflated := image.Rect(r.Min.X-8, r.Min.Y-8, r.Max.X+8, r.Max.Y+8)
			if inflated.Overlaps(trueRect) || inflated.Overlaps(pieceRect) {
				continue
			}
			overlap := false
			for _, h := range holes {
				if inflated.Overlaps(h) {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			ok = true
			break
		}
		if !ok {
			continue
		}
		decoy := image.Rect(dx, dy, dx+pw, dy+ph)
		decoyMask := polygonMaskN(pw, ph, rnd, 5+rnd.Intn(4))
		drawHole(bg, decoyMask, dx, dy, pw, ph, style, rnd, pristine)
		holes = append(holes, decoy)
		actualDecoys++
	}

	drawInterference(bg, rnd, holes)

	return &GenResult{
		Background: bg,
		Piece:      piece,
		TargetX:    targetX,
		TargetY:    targetY,
		PieceX:     pieceX,
		PieceW:     pw,
		PieceH:     ph,
		Decoys:     actualDecoys,
	}, nil
}

func paintBackground(rnd *rand.Rand, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hue := rnd.Float64() * 360

	c1 := hsv(hue, 0.28+rnd.Float64()*0.20, 0.97)
	c2 := hsv(math.Mod(hue+20+rnd.Float64()*70, 360), 0.34+rnd.Float64()*0.22, 0.72+rnd.Float64()*0.14)
	c3 := hsv(math.Mod(hue+150+rnd.Float64()*90, 360), 0.26+rnd.Float64()*0.20, 0.96)

	ang := rnd.Float64() * math.Pi
	dx, dy := math.Cos(ang), math.Sin(ang)
	minP, maxP := math.Inf(1), math.Inf(-1)
	for _, cx := range []float64{0, float64(w)} {
		for _, cy := range []float64{0, float64(h)} {
			p := cx*dx + cy*dy
			minP = math.Min(minP, p)
			maxP = math.Max(maxP, p)
		}
	}
	if maxP == minP {
		maxP = minP + 1
	}

	streak := rnd.Float64()*float64(w)*0.5 + float64(w)*0.25
	streaky := rnd.Float64()*float64(h)*0.6 + float64(h)*0.2
	maxR := math.Hypot(float64(w), float64(h)) * 0.75

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)*dx + float64(y)*dy - minP) / (maxP - minP)
			c := lerpColor(c1, c2, t)

			d := math.Hypot(float64(x)-streak, float64(y)-streaky) / maxR
			if d < 1 {
				c = lerpColor(c, c3, 0.42*(1-d)*(1-d))
			}

			c = lerpColor(c, c3, 0.06+0.05*math.Sin(float64(x)*0.05+float64(y)*0.03))

			i := img.PixOffset(x, y)
			img.Pix[i] = c.R
			img.Pix[i+1] = c.G
			img.Pix[i+2] = c.B
			img.Pix[i+3] = 255
		}
	}

	for i := 0; i < 18+rnd.Intn(14); i++ {
		radius := 8 + rnd.Float64()*float64(min(w, h))*0.42
		cx := rnd.Float64() * float64(w)
		cy := rnd.Float64() * float64(h)
		col := hsv(math.Mod(hue+(rnd.Float64()-0.5)*140+360, 360), 0.26+rnd.Float64()*0.38, 0.74+rnd.Float64()*0.24)
		fillCircle(img, cx, cy, radius, col, 0.03+rnd.Float64()*0.10)
	}

	for i := 0; i < 4+rnd.Intn(4); i++ {
		x1 := rnd.Float64() * float64(w)
		y1 := rnd.Float64() * float64(h)
		x2 := rnd.Float64() * float64(w)
		y2 := rnd.Float64() * float64(h)
		col := hsv(math.Mod(hue+(rnd.Float64()-0.5)*60+360, 360), 0.2+rnd.Float64()*0.3, 0.75+rnd.Float64()*0.25)
		drawLine(img, x1, y1, x2, y2, 0.8+rnd.Float64()*1.2, col, 0.03+rnd.Float64()*0.05)
	}

	for i := 0; i < len(img.Pix); i += 4 {
		n := int(rnd.NormFloat64() * 2.6)
		img.Pix[i] = uint8(clamp8(int(img.Pix[i]) + n))
		img.Pix[i+1] = uint8(clamp8(int(img.Pix[i+1]) + n))
		img.Pix[i+2] = uint8(clamp8(int(img.Pix[i+2]) + n))
	}

	addValueNoise(img, rnd, 18, 9)
	addValueNoise(img, rnd, 6, 4)

	cx, cy := float64(w)/2, float64(h)/2
	maxD := math.Hypot(cx, cy)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / maxD
			f := 1 - 0.16*d*d
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(clamp8(int(float64(img.Pix[i]) * f)))
			img.Pix[i+1] = uint8(clamp8(int(float64(img.Pix[i+1]) * f)))
			img.Pix[i+2] = uint8(clamp8(int(float64(img.Pix[i+2]) * f)))
		}
	}

	// 背景太暗会让缺口太显眼，先把暗部提亮。
	liftDarkPixels(img)
	return img
}

func liftDarkPixels(img *image.NRGBA) {
	const floorLum = 62.0
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
		lum := 0.299*r + 0.587*g + 0.114*b
		if lum >= floorLum {
			continue
		}
		t := (floorLum - lum) / floorLum
		f := 0.70 * t
		img.Pix[i] = uint8(clamp8(int(r + (246-r)*f)))
		img.Pix[i+1] = uint8(clamp8(int(g + (244-g)*f)))
		img.Pix[i+2] = uint8(clamp8(int(b + (238-b)*f)))
	}
}

func polygonMaskN(w, h int, rnd *rand.Rand, n int) []uint8 {
	return rasterPolygon(w, h, polygonPoints(w, h, rnd, n))
}

func polygonPoints(w, h int, rnd *rand.Rand, n int) []pointF {
	if n < 3 {
		n = 3
	}
	if n > 12 {
		n = 12
	}
	cx := float64(w) / 2
	cy := float64(h) / 2
	base := math.Min(float64(w), float64(h))/2 - 1

	pts := make([]pointF, 0, n)
	start := rnd.Float64() * 2 * math.Pi
	for i := 0; i < n; i++ {
		ang := start + 2*math.Pi*float64(i)/float64(n) + (rnd.Float64()-0.5)*0.30
		var r float64
		if i%2 == 0 {
			r = base * (0.62 + rnd.Float64()*0.38)
		} else {
			r = base * (0.70 + rnd.Float64()*0.24)
		}
		pts = append(pts, pointF{cx + r*math.Cos(ang), cy + r*math.Sin(ang)})
	}
	return pts
}

func perturbPolygon(pts []pointF, w, h int, rnd *rand.Rand) []pointF {
	out := append([]pointF(nil), pts...)
	if len(out) == 0 {
		return out
	}
	i := rnd.Intn(len(out))
	cx, cy := float64(w)/2, float64(h)/2
	vx, vy := out[i].x-cx, out[i].y-cy
	d := math.Hypot(vx, vy)
	if d < 1 {
		return out
	}
	delta := 2.0 + rnd.Float64()*3.0
	out[i].x -= vx / d * delta
	out[i].y -= vy / d * delta
	out[i].x = math.Min(float64(w)-1, math.Max(1, out[i].x))
	out[i].y = math.Min(float64(h)-1, math.Max(1, out[i].y))
	return out
}

func rasterPolygon(w, h int, pts []pointF) []uint8 {
	const ss = 4
	inv := 1.0 / float64(ss*ss)
	cov := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inside := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					if pointInPoly(px, py, pts) {
						inside++
					}
				}
			}
			cov[y*w+x] = uint8(float64(inside)*inv*255 + 0.5)
		}
	}
	return cov
}

func cutPiece(src *image.NRGBA, mask []uint8, tx, ty, pw, ph int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			cv := mask[y*pw+x]
			if cv == 0 {
				continue
			}
			sx, sy := tx+x, ty+y
			if sx < 0 || sy < 0 || sx >= sw || sy >= sh {
				continue
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			dst.Pix[di] = src.Pix[si]
			dst.Pix[di+1] = src.Pix[si+1]
			dst.Pix[di+2] = src.Pix[si+2]
			dst.Pix[di+3] = cv
		}
	}
	return dst
}

// softenPiece 让下发的碎片不能直接当模板去背景里匹配：
// 亚像素正弦形变、3x3 模糊、各通道色偏和噪点，轮廓 alpha 保持不变。
func softenPiece(img *image.NRGBA, rnd *rand.Rand) *image.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	// 预乘 alpha，形变和模糊时透明像素才不会把边缘拉黑。
	pm := make([]float64, w*h*4)
	for i := 0; i < len(img.Pix); i += 4 {
		a := float64(img.Pix[i+3]) / 255
		pm[i] = float64(img.Pix[i]) * a
		pm[i+1] = float64(img.Pix[i+1]) * a
		pm[i+2] = float64(img.Pix[i+2]) * a
		pm[i+3] = a
	}
	at := func(x, y int) int { return (y*w + x) * 4 }
	sample := func(sx, sy float64) [4]float64 {
		sx = math.Max(0, math.Min(float64(w-1), sx))
		sy = math.Max(0, math.Min(float64(h-1), sy))
		x0, y0 := int(sx), int(sy)
		x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
		tx, ty := sx-float64(x0), sy-float64(y0)
		var out [4]float64
		for c := 0; c < 4; c++ {
			top := pm[at(x0, y0)+c]*(1-tx) + pm[at(x1, y0)+c]*tx
			bot := pm[at(x0, y1)+c]*(1-tx) + pm[at(x1, y1)+c]*tx
			out[c] = top*(1-ty) + bot*ty
		}
		return out
	}

	ampX := 0.6 + rnd.Float64()*0.6
	ampY := 0.6 + rnd.Float64()*0.6
	fx := 2 * math.Pi / float64(12+rnd.Intn(24))
	fy := 2 * math.Pi / float64(12+rnd.Intn(24))
	px := rnd.Float64() * 2 * math.Pi
	py := rnd.Float64() * 2 * math.Pi
	var kernel = [3][3]float64{{1, 2, 1}, {2, 4, 2}, {1, 2, 1}}

	gainR := 0.88 + rnd.Float64()*0.24
	gainG := 0.88 + rnd.Float64()*0.24
	gainB := 0.88 + rnd.Float64()*0.24
	offR := rnd.Float64()*16 - 8
	offG := rnd.Float64()*16 - 8
	offB := rnd.Float64()*16 - 8

	out := image.NewNRGBA(img.Rect)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			if img.Pix[i+3] == 0 {
				continue
			}
			var acc [4]float64
			for ky := -1; ky <= 1; ky++ {
				for kx := -1; kx <= 1; kx++ {
					wt := kernel[kx+1][ky+1]
					sx := float64(x+kx) + ampX*math.Sin(fy*float64(y)+py)
					sy := float64(y+ky) + ampY*math.Sin(fx*float64(x)+px)
					s := sample(sx, sy)
					for c := 0; c < 4; c++ {
						acc[c] += s[c] * wt
					}
				}
			}
			o := out.PixOffset(x, y)
			if acc[3] > 1e-3 {
				n := rnd.NormFloat64() * 3
				out.Pix[o] = uint8(clamp8(int(acc[0]/acc[3]*gainR + offR + n)))
				out.Pix[o+1] = uint8(clamp8(int(acc[1]/acc[3]*gainG + offG + n)))
				out.Pix[o+2] = uint8(clamp8(int(acc[2]/acc[3]*gainB + offB + n)))
			}
			out.Pix[o+3] = img.Pix[i+3]
		}
	}
	return out
}

func sameRowDecoyX(w, targetX, targetY, pw, ph, pieceX, gap, side int) (int, bool) {
	if pw <= 0 || ph <= 0 || w <= 0 {
		return 0, false
	}
	trueRect := image.Rect(targetX, targetY, targetX+pw, targetY+ph)
	pieceRect := image.Rect(pieceX, targetY, pieceX+pw, targetY+ph)

	try := func(x int) (int, bool) {
		if x < 8 || x+pw+8 > w {
			return 0, false
		}
		r := image.Rect(x, targetY, x+pw, targetY+ph)
		inflated := image.Rect(r.Min.X-8, r.Min.Y-8, r.Max.X+8, r.Max.Y+8)
		if inflated.Overlaps(trueRect) || inflated.Overlaps(pieceRect) {
			return 0, false
		}
		return x, true
	}

	if side == 0 {
		if x, ok := try(targetX - pw - gap); ok {
			return x, true
		}
	} else {
		if x, ok := try(targetX + pw + gap); ok {
			return x, true
		}
	}
	if side == 0 {
		return try(targetX + pw + gap)
	}
	return try(targetX - pw - gap)
}

func sameRowTargetX(rnd *rand.Rand, w, pw, pieceX, gap, side int) (int, bool) {
	minTarget := pieceX + 60 + pw
	maxTarget := w - pw - 12
	if side == 0 {
		minTarget = pieceX + 2*pw + gap + 8
	} else {
		maxTarget = w - 2*pw - gap - 8
	}
	if minTarget > maxTarget {
		return 0, false
	}
	return randomTargetX(rnd, minTarget, maxTarget), true
}

func randomTargetX(rnd *rand.Rand, minTarget, maxTarget int) int {
	if minTarget > maxTarget {
		minTarget = maxTarget
	}
	x := minTarget
	if span := maxTarget - minTarget; span > 0 {
		x += rnd.Intn(span + 1)
	}
	return x
}

func drawHole(img *image.NRGBA, mask []uint8, tx, ty, pw, ph int, st holeStyle, rnd *rand.Rand, src *image.NRGBA) {
	sx, sy := distantSample(rnd, img.Rect.Dx(), img.Rect.Dy(), tx, ty, pw, ph)
	drawHoleFrom(img, mask, tx, ty, pw, ph, st, rnd, src, sx, sy)
}

// 缺口亮度取本地，纹理从远处取。本地的原始像素一点不留，
// 否则碎片本身就是缺口的像素级模板。
func drawHoleFrom(img *image.NRGBA, mask []uint8, tx, ty, pw, ph int, st holeStyle, rnd *rand.Rand, src *image.NRGBA, sampleX, sampleY int) {
	if src == nil {
		src = img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	avgR, avgG, avgB := regionAverage(src, tx, ty, pw, ph)

	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			cv := mask[y*pw+x]
			// 覆盖率太低的边缘像素直接不画，否则会透出本地背景。
			if cv < 8 {
				continue
			}
			dx, dy := tx+x, ty+y
			if dx < 0 || dy < 0 || dx >= w || dy >= h {
				continue
			}

			alpha := float64(cv) / 255
			if cv < 243 {
				alpha += (rnd.Float64() - 0.5) * 0.22
			}
			if alpha < 0 {
				alpha = 0
			}
			if alpha > 1 {
				alpha = 1
			}

			sx, sy := sampleX+x+st.offX, sampleY+y+st.offY
			if sx < 0 {
				sx = 0
			}
			if sy < 0 {
				sy = 0
			}
			if sx >= src.Rect.Dx() {
				sx = src.Rect.Dx() - 1
			}
			if sy >= src.Rect.Dy() {
				sy = src.Rect.Dy() - 1
			}
			si := src.PixOffset(sx, sy)

			r0 := float64(src.Pix[si])
			g0 := float64(src.Pix[si+1])
			b0 := float64(src.Pix[si+2])
			// 边缘先铺远处采样色再叠缺口色，抗锯齿圈里也不能有本地像素。
			if cv < 243 {
				blendAt(img, dx, dy, color.NRGBA{R: uint8(r0), G: uint8(g0), B: uint8(b0), A: 255}, 1)
			}
			var r, g, b float64
			if st.mode == 1 {
				lum := 0.299*r0 + 0.587*g0 + 0.114*b0
				base := lum*0.90 + 3
				r = base*(1-st.texture) + r0*st.texture + st.tintR*0.35
				g = base*(1-st.texture) + g0*st.texture + st.tintG*0.35
				b = base*(1-st.texture) + b0*st.texture + st.tintB*0.35
			} else {
				r = (r0*st.texture+avgR*(1-st.texture))*st.shade + st.tintR
				g = (g0*st.texture+avgG*(1-st.texture))*st.shade + st.tintG
				b = (b0*st.texture+avgB*(1-st.texture))*st.shade + st.tintB
			}
			blendAt(img, dx, dy, color.NRGBA{
				R: uint8(clamp8(int(r))),
				G: uint8(clamp8(int(g))),
				B: uint8(clamp8(int(b))),
				A: 255,
			}, alpha)

			if st.rimMode != 0 && isMaskEdge(mask, x, y, pw, ph) {
				a := alpha * st.rimAlpha
				if st.rimMode == 1 && !isMaskEdgeTop(mask, x, y, pw, ph) {
					a *= 0.45
				}
				blendAt(img, dx, dy, st.rimColor, a)
			}
		}
	}
}

// distantSample 挑一块离缺口足够远的区域当纹理来源。
func distantSample(rnd *rand.Rand, w, h, tx, ty, pw, ph int) (int, int) {
	minDist := max(pw, ph) + 16
	for tries := 0; tries < 16; tries++ {
		x := rnd.Intn(max(1, w-pw))
		y := rnd.Intn(max(1, h-ph))
		if absInt(x-tx) >= minDist || absInt(y-ty) >= minDist {
			return x, y
		}
	}
	return (tx + w/2) % max(1, w-pw), (ty + h/2) % max(1, h-ph)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type holeStyle struct {
	mode     int     // 0 暗洞 1 雾化
	shade    float64 // 内部明暗系数
	tintR    float64
	tintG    float64
	tintB    float64
	rimMode  int // 0 无描边 1 亮边 2 暗边 3 彩色边
	rimColor color.NRGBA
	rimAlpha float64
	texture  float64 // 缺口内保留的原始纹理比例：0 为纯色遮罩，1 为完全延续
	offX     int
	offY     int
}

func newHoleStyle(rnd *rand.Rand) holeStyle {
	st := holeStyle{}
	if rnd.Float64() < 0.45 {
		st.mode = 1
		st.shade = 1.0
		st.texture = 0.05 + rnd.Float64()*0.1
	} else {
		st.mode = 0
		st.shade = 0.46 + rnd.Float64()*0.24
		st.texture = 0.12 + rnd.Float64()*0.22
	}
	st.tintR = (rnd.Float64() - 0.5) * 26
	st.tintG = (rnd.Float64() - 0.5) * 26
	st.tintB = (rnd.Float64() - 0.5) * 26

	switch r := rnd.Float64(); {
	case r < 0.10:
		st.rimMode = 0
	case r < 0.60:
		st.rimMode = 1
	case r < 0.85:
		st.rimMode = 2
	default:
		st.rimMode = 3
	}
	st.rimAlpha = 0.20 + rnd.Float64()*0.24
	if st.mode == 1 {
		st.rimMode = 2
		st.rimAlpha = 0.34 + rnd.Float64()*0.2
	}
	st.rimColor = color.NRGBA{R: 245, G: 245, B: 245, A: 255}
	switch st.rimMode {
	case 2:
		st.rimColor = color.NRGBA{R: 14, G: 14, B: 18, A: 255}
	case 3:
		st.rimColor = color.NRGBA{R: uint8(110 + rnd.Intn(120)), G: uint8(110 + rnd.Intn(120)), B: uint8(110 + rnd.Intn(120)), A: 255}
		st.rimAlpha *= 0.6
	}

	st.offX = rnd.Intn(7) - 3
	st.offY = rnd.Intn(7) - 3
	if st.offX == 0 && st.offY == 0 {
		st.offY = 2
	}
	return st
}

func drawInterference(img *image.NRGBA, rnd *rand.Rand, holes []image.Rectangle) {
	for _, hole := range holes {
		if rnd.Float64() < 0.2 {
			continue
		}
		y0 := float64(hole.Min.Y) + rnd.Float64()*float64(hole.Dy())
		x0 := float64(hole.Min.X) - 10 - rnd.Float64()*18
		x1 := float64(hole.Max.X) + 10 + rnd.Float64()*18
		y1 := y0 + (rnd.Float64()-0.5)*14
		col := color.NRGBA{
			R: uint8(30 + rnd.Intn(200)),
			G: uint8(30 + rnd.Intn(200)),
			B: uint8(30 + rnd.Intn(200)),
			A: 255,
		}
		drawLine(img, x0, y0, x1, y1, 0.7+rnd.Float64()*1.0, col, 0.05+rnd.Float64()*0.07)
	}
}

func regionAverage(src *image.NRGBA, x0, y0, w0, h0 int) (float64, float64, float64) {
	bw, bh := src.Rect.Dx(), src.Rect.Dy()
	var r, g, b, n float64
	for y := y0; y < y0+h0; y++ {
		if y < 0 || y >= bh {
			continue
		}
		for x := x0; x < x0+w0; x++ {
			if x < 0 || x >= bw {
				continue
			}
			i := src.PixOffset(x, y)
			r += float64(src.Pix[i])
			g += float64(src.Pix[i+1])
			b += float64(src.Pix[i+2])
			n++
		}
	}
	if n == 0 {
		return 128, 128, 128
	}
	return r / n, g / n, b / n
}

func addValueNoise(img *image.NRGBA, rnd *rand.Rand, scale int, amp float64) {
	if scale < 1 {
		scale = 1
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	gw, gh := w/scale+2, h/scale+2
	grid := make([]float64, gw*gh)
	for i := range grid {
		grid[i] = rnd.Float64()*2 - 1
	}
	for y := 0; y < h; y++ {
		fy := float64(y) / float64(scale)
		iy := int(fy)
		ty := smoothstep(fy - float64(iy))
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(scale)
			ix := int(fx)
			tx := smoothstep(fx - float64(ix))
			v00 := grid[iy*gw+ix]
			v10 := grid[iy*gw+ix+1]
			v01 := grid[(iy+1)*gw+ix]
			v11 := grid[(iy+1)*gw+ix+1]
			top := v00 + (v10-v00)*tx
			bottom := v01 + (v11-v01)*tx
			v := top + (bottom-top)*ty
			i := img.PixOffset(x, y)
			n := int(v * amp)
			img.Pix[i] = uint8(clamp8(int(img.Pix[i]) + n))
			img.Pix[i+1] = uint8(clamp8(int(img.Pix[i+1]) + n))
			img.Pix[i+2] = uint8(clamp8(int(img.Pix[i+2]) + n))
		}
	}
}

func smoothstep(t float64) float64 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

func isMaskEdge(mask []uint8, x, y, w, h int) bool {
	v := mask[y*w+x]
	if v == 0 {
		return false
	}
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= w || ny >= h {
			return true
		}
		if mask[ny*w+nx] < 150 {
			return true
		}
	}
	return false
}

func isMaskEdgeTop(mask []uint8, x, y, w, h int) bool {
	if mask[y*w+x] == 0 {
		return false
	}
	if y == 0 {
		return true
	}
	return mask[(y-1)*w+x] < 150
}

type pointF struct{ x, y float64 }

func pointInPoly(px, py float64, pts []pointF) bool {
	inside := false
	n := len(pts)
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := pts[i].x, pts[i].y
		xj, yj := pts[j].x, pts[j].y
		if (yi > py) != (yj > py) {
			at := (xj-xi)*(py-yi)/(yj-yi) + xi
			if px < at {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func fillCircle(img *image.NRGBA, cx, cy, r float64, c color.NRGBA, a float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	x0 := int(math.Floor(cx - r))
	x1 := int(math.Ceil(cx + r))
	y0 := int(math.Floor(cy - r))
	y1 := int(math.Ceil(cy + r))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > w-1 {
		x1 = w - 1
	}
	if y1 > h-1 {
		y1 = h - 1
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			edge := r - d
			if edge <= -1 {
				continue
			}
			aa := 1.0
			if edge < 1 {
				aa = edge + 1
			}
			blendAt(img, x, y, c, a*aa)
		}
	}
}

func drawLine(img *image.NRGBA, x1, y1, x2, y2, width float64, c color.NRGBA, a float64) {
	dist := math.Hypot(x2-x1, y2-y1)
	steps := int(dist * 2)
	if steps < 1 {
		steps = 1
	}
	if steps > 4000 {
		steps = 4000
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x1 + (x2-x1)*t
		y := y1 + (y2-y1)*t
		fillCircle(img, x, y, width/2, c, a)
	}
}

func blendAt(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if x < 0 || y < 0 || x >= w || y >= h {
		return
	}
	i := img.PixOffset(x, y)
	pa := float64(c.A) / 255 * a
	dstA := float64(img.Pix[i+3])

	img.Pix[i] = uint8(clamp8(int(float64(img.Pix[i])*(1-pa) + float64(c.R)*pa)))
	img.Pix[i+1] = uint8(clamp8(int(float64(img.Pix[i+1])*(1-pa) + float64(c.G)*pa)))
	img.Pix[i+2] = uint8(clamp8(int(float64(img.Pix[i+2])*(1-pa) + float64(c.B)*pa)))
	img.Pix[i+3] = uint8(clamp8(int(dstA*(1-pa) + 255*pa)))
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, src.Rect.Dx(), src.Rect.Dy()))
	copy(dst.Pix, src.Pix)
	return dst
}

func hsv(h, s, v float64) color.NRGBA {
	h = math.Mod(math.Mod(h, 360)+360, 360)
	s = math.Min(1, math.Max(0, s))
	v = math.Min(1, math.Max(0, v))

	c := v * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch int(hp) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := v - c
	return color.NRGBA{
		R: uint8(clamp8(int((r + m) * 255))),
		G: uint8(clamp8(int((g + m) * 255))),
		B: uint8(clamp8(int((b + m) * 255))),
		A: 255,
	}
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.NRGBA{
		R: uint8(clamp8(int(float64(a.R)*(1-t) + float64(b.R)*t))),
		G: uint8(clamp8(int(float64(a.G)*(1-t) + float64(b.G)*t))),
		B: uint8(clamp8(int(float64(a.B)*(1-t) + float64(b.B)*t))),
		A: 255,
	}
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
