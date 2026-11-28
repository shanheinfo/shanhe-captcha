package shanhecaptcha

import (
	"context"
	"image"
	"image/png"
	"math"
	"math/rand"
	"os"
	"testing"
)

// 把 attacksim 里的模板匹配搬过来，量 softenPiece 前后的命中率。
func nccMatchX(bg *image.NRGBA, piece *image.NRGBA, rowY int) int {
	bw := bg.Rect.Dx()
	pw, ph := piece.Rect.Dx(), piece.Rect.Dy()

	var sumP, sumP2, cnt float64
	pg := make([]float64, pw*ph)
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := piece.PixOffset(x, y)
			if piece.Pix[i+3] < 128 {
				continue
			}
			v := 0.299*float64(piece.Pix[i]) + 0.587*float64(piece.Pix[i+1]) + 0.114*float64(piece.Pix[i+2])
			pg[y*pw+x] = v
			sumP += v
			sumP2 += v * v
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	muP := sumP / cnt
	varP := sumP2 - cnt*muP*muP
	if varP <= 0 {
		return 0
	}

	bestX, best := 0, -2.0
	for x := 0; x+pw <= bw; x++ {
		var sumW, sumW2, sumPW float64
		n := 0.0
		for y := 0; y < ph; y++ {
			yy := rowY + y
			if yy < 0 || yy >= bg.Rect.Dy() {
				continue
			}
			for xx := 0; xx < pw; xx++ {
				p := pg[y*pw+xx]
				if p == 0 {
					continue
				}
				i := bg.PixOffset(x+xx, yy)
				wv := 0.299*float64(bg.Pix[i]) + 0.587*float64(bg.Pix[i+1]) + 0.114*float64(bg.Pix[i+2])
				sumW += wv
				sumW2 += wv * wv
				sumPW += p * wv
				n++
			}
		}
		if n == 0 {
			continue
		}
		muW := sumW / n
		varW := sumW2 - n*muW*muW
		if varW <= 0 {
			continue
		}
		cov := sumPW - n*muP*muW
		if ncc := cov / math.Sqrt(varP*varW); ncc > best {
			best, bestX = ncc, x
		}
	}
	return bestX
}

func TestPieceTemplateMatchRate(t *testing.T) {
	rnd := rand.New(rand.NewSource(20261128))
	const n, tol = 80, 4
	raw, shipped := 0, 0
	for i := 0; i < n; i++ {
		res, err := DefaultGenerator{}.Generate(context.Background(), GenRequest{
			Width: 320, Height: 160, PieceSize: 48, Decoys: 3, Rand: rnd,
		})
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(float64(nccMatchX(res.Background, res.Piece, res.TargetY)-res.TargetX)) <= tol {
			raw++
		}
		if math.Abs(float64(nccMatchX(res.Background, softenPiece(res.Piece, rnd), res.TargetY)-res.TargetX)) <= tol {
			shipped++
		}
	}
	t.Logf("n=%d  raw=%d  shipped=%d", n, raw, shipped)
	if shipped > n/5 {
		t.Fatalf("下发碎片的模板命中率应低于 20%%，实际 %d/%d", shipped, n)
	}
}

// 写几张软化后的碎片到 SHC_VISUAL 目录，方便肉眼看效果。
func TestSoftenPieceVisual(t *testing.T) {
	dir := os.Getenv("SHC_VISUAL")
	if dir == "" {
		t.Skip("set SHC_VISUAL=dir to write samples")
	}
	rnd := rand.New(rand.NewSource(7))
	for i := 0; i < 4; i++ {
		res, _ := DefaultGenerator{}.Generate(context.Background(), GenRequest{
			Width: 320, Height: 160, PieceSize: 48, Decoys: 3, Rand: rnd,
		})
		sp := softenPiece(res.Piece, rnd)
		canvas := image.NewNRGBA(image.Rect(0, 0, 320, 160))
		copy(canvas.Pix, res.Background.Pix)
		// 左边原图，右边软化后。
		for _, p := range []*image.NRGBA{res.Piece, sp} {
			ox := res.PieceX
			if p == sp {
				ox = res.PieceX + 60
			}
			for y := 0; y < p.Rect.Dy(); y++ {
				for x := 0; x < p.Rect.Dx(); x++ {
					si := p.PixOffset(x, y)
					a := int(p.Pix[si+3])
					if a == 0 {
						continue
					}
					di := canvas.PixOffset(ox+x, res.TargetY+y)
					for c := 0; c < 3; c++ {
						canvas.Pix[di+c] = uint8((int(p.Pix[si+c])*a + int(canvas.Pix[di+c])*(255-a)) / 255)
					}
				}
			}
		}
		f, _ := os.CreateTemp(dir, "soften-*.png")
		png.Encode(f, canvas)
		f.Close()
		t.Log(f.Name())
	}
}
