// Command attacksim 用几种简单方法找缺口，输出命中率。
//
// 它只代表低成本脚本的基线，不是完整的安全测试。
//
//	go run ./cmd/attacksim -n 60 -decoys 3
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"

	shanhecaptcha "github.com/shanheinfo/shanhe-captcha/backend-go"
)

func main() {
	n := flag.Int("n", 60, "每种配置生成多少张")
	w := flag.Int("w", 320, "宽")
	h := flag.Int("h", 160, "高")
	tol := flag.Int("tol", 4, "判定命中允许的像素误差")
	decoys := flag.Int("decoys", 0, "假缺口数量（0~3）")
	flag.Parse()

	gen := shanhecaptcha.DefaultGenerator{}
	ctx := context.Background()

	var darkHit, flatHit, oddHit, nccHit, anyHit int
	for i := 0; i < *n; i++ {
		res, err := gen.Generate(ctx, shanhecaptcha.GenRequest{
			Width:     *w,
			Height:    *h,
			PieceSize: 48,
			Decoys:    *decoys,
			Rand:      rand.New(rand.NewSource(int64(i)*7919 + 13)),
		})
		if err != nil {
			panic(err)
		}

		darkX := solveBand(res, false)
		flatX := solveBand(res, true)
		oddX := solveOdd(res)
		nccX := solveNCC(res)

		if abs(darkX-res.TargetX) <= *tol {
			darkHit++
		}
		if abs(flatX-res.TargetX) <= *tol {
			flatHit++
		}
		if abs(oddX-res.TargetX) <= *tol {
			oddHit++
		}
		if abs(nccX-res.TargetX) <= *tol {
			nccHit++
		}
		if abs(darkX-res.TargetX) <= *tol || abs(flatX-res.TargetX) <= *tol || abs(oddX-res.TargetX) <= *tol || abs(nccX-res.TargetX) <= *tol {
			anyHit++
		}
	}

	pct := func(k int) float64 { return float64(k) * 100 / float64(*n) }
	fmt.Printf("n=%d  最暗 %.0f%%  最平 %.0f%%  局部异常 %.0f%%  模板匹配 %.0f%%  取一 %.0f%%\n",
		*n, pct(darkHit), pct(flatHit), pct(oddHit), pct(nccHit), pct(anyHit))
}

// solveBand 在碎片所在行横向滑窗，找最可疑的 x。
func solveBand(res *shanhecaptcha.GenResult, flat bool) int {
	img := res.Background
	bw := img.Rect.Dx()
	pw, ph := res.PieceW, res.PieceH
	y0, y1 := res.TargetY, res.TargetY+ph
	if y0 < 0 {
		y0 = 0
	}
	if y1 > img.Rect.Dy() {
		y1 = img.Rect.Dy()
	}
	if y1 <= y0 || pw >= bw {
		return 0
	}

	rows := float64(y1 - y0)
	colSum := make([]float64, bw)
	colSq := make([]float64, bw)
	for x := 0; x < bw; x++ {
		for y := y0; y < y1; y++ {
			i := img.PixOffset(x, y)
			l := 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
			colSum[x] += l
			colSq[x] += l * l
		}
	}

	pref := make([]float64, bw+1)
	prefSq := make([]float64, bw+1)
	for x := 0; x < bw; x++ {
		pref[x+1] = pref[x] + colSum[x]
		prefSq[x+1] = prefSq[x] + colSq[x]
	}

	bestX, bestScore := 0, math.Inf(1)
	count := rows * float64(pw)
	for x := 0; x+pw <= bw; x++ {
		s := pref[x+pw] - pref[x]
		sq := prefSq[x+pw] - prefSq[x]
		mean := s / count
		if flat {
			v := sq/count - mean*mean
			if v < bestScore {
				bestScore, bestX = v, x
			}
			continue
		}
		if mean < bestScore {
			bestScore, bestX = mean, x
		}
	}
	return bestX
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// solveNCC 把碎片当模板，在碎片所在行找最像的位置。
func solveNCC(res *shanhecaptcha.GenResult) int {
	img := res.Background
	bw := img.Rect.Dx()
	pw, ph := res.PieceW, res.PieceH
	piece := res.Piece

	var sumP, sumP2 float64
	var cnt float64
	pg := make([]float64, pw*ph)
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := piece.PixOffset(x, y)
			a := piece.Pix[i+3]
			if a < 128 {
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
			yy := res.TargetY + y
			if yy < 0 || yy >= img.Rect.Dy() {
				continue
			}
			for xx := 0; xx < pw; xx++ {
				p := pg[y*pw+xx]
				if p == 0 {
					continue
				}
				i := img.PixOffset(x+xx, yy)
				wv := 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
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
		ncc := cov / math.Sqrt(varP*varW)
		if ncc > best {
			best, bestX = ncc, x
		}
	}
	return bestX
}

// solveOdd 找和周围差异最大的窗口。
func solveOdd(res *shanhecaptcha.GenResult) int {
	img := res.Background
	bw := img.Rect.Dx()
	pw, ph := res.PieceW, res.PieceH
	y0 := res.TargetY
	y1 := y0 + ph
	if y0 < 0 {
		y0 = 0
	}
	if y1 > img.Rect.Dy() {
		y1 = img.Rect.Dy()
	}
	if y1 <= y0 || pw >= bw {
		return 0
	}

	rows := float64(y1 - y0)
	col := make([]float64, bw)
	for x := 0; x < bw; x++ {
		for y := y0; y < y1; y++ {
			i := img.PixOffset(x, y)
			col[x] += 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
		}
	}
	pref := make([]float64, bw+1)
	for x := 0; x < bw; x++ {
		pref[x+1] = pref[x] + col[x]
	}
	sum := func(a, b int) float64 { return pref[b] - pref[a] }
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}

	bestX, best := 0, -1.0
	for x := 0; x+pw <= bw; x++ {
		inner := sum(x, x+pw) / (rows * float64(pw))
		a := clamp(x-10, 0, bw)
		b := clamp(x+pw+10, 0, bw)
		outer := (sum(a, b) - sum(x, x+pw)) / (rows * float64(b-a-pw))
		d := math.Abs(inner - outer)
		if d > best {
			best, bestX = d, x
		}
	}
	return bestX
}
