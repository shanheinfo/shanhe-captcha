package shanhecaptcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCaptcha(t *testing.T, mutate func(*Config)) (*Captcha, *httptest.Server) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.MinSolveDuration = time.Nanosecond
	cfg.Debug = true
	cfg.RateLimit.Enabled = false
	// 大部分用例只看位置和票据，行为评分单独测。
	cfg.Behavior.Enabled = false
	if mutate != nil {
		mutate(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(c.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = c.Close()
	})
	return c, srv
}

func getChallenge(t *testing.T, srv *httptest.Server) *Challenge {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/captcha/challenge")
	if err != nil {
		t.Fatalf("GET challenge: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("challenge status = %d", resp.StatusCode)
	}
	var ch Challenge
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	if ch.ID == "" {
		t.Fatal("empty challenge id")
	}
	return &ch
}

func answerOf(t *testing.T, c *Captcha, id string) float64 {
	t.Helper()
	raw, err := c.store.Get(context.Background(), challengeKeyPrefix+id)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec challengeRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return rec.AnswerX
}

func makeTrail(dest float64, n int) []TrailPoint {
	if n < 2 {
		n = 2
	}
	trail := make([]TrailPoint, 0, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		// 轻微抖动，模拟正常指针输入。
		jitter := math.Sin(float64(i)*1.7) * 0.6
		trail = append(trail, TrailPoint{
			T: int64(i * 16),
			X: dest*f + jitter,
			Y: math.Sin(float64(i)*0.9) * 2,
		})
	}
	trail[len(trail)-1].X = dest
	return trail
}

func postVerify(t *testing.T, srv *httptest.Server, req VerifyRequest) VerifyResult {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := srv.Client().Post(srv.URL+"/captcha/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST verify: %v", err)
	}
	defer resp.Body.Close()
	var res VerifyResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	return res
}

func TestChallengePayloadHasNoAnswer(t *testing.T) {
	_, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)

	// 图像必须是合法的 JPEG/PNG 数据。
	for name, uri := range map[string]string{"background": ch.Background, "piece": ch.Piece} {
		if !strings.HasPrefix(uri, "data:image/") {
			t.Fatalf("%s 不是 data URI", name)
		}
		comma := strings.IndexByte(uri, ',')
		if comma < 0 {
			t.Fatalf("%s 缺少 base64 分隔符", name)
		}
		raw, err := base64.StdEncoding.DecodeString(uri[comma+1:])
		if err != nil {
			t.Fatalf("%s base64: %v", name, err)
		}
		if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("%s 解码失败: %v", name, err)
		}
	}

	// 序列化后不能出现答案字段。
	out, _ := json.Marshal(ch)
	for _, bad := range []string{"answer", "target", "targetX"} {
		if strings.Contains(string(out), bad) {
			t.Fatalf("下发数据里出现了敏感字段 %q: %s", bad, out)
		}
	}
}

func TestFullFlow(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{
		ID:       ch.ID,
		X:        answer,
		Duration: 500,
		Trail:    makeTrail(answer, 20),
	})
	if !res.OK {
		t.Fatalf("期望通过，实际失败: %+v", res)
	}
	if res.Ticket == "" {
		t.Fatal("没有签发 ticket")
	}

	info, err := c.ConsumeTicket(context.Background(), res.Ticket)
	if err != nil {
		t.Fatalf("ConsumeTicket: %v", err)
	}
	if info.ChallengeID != ch.ID {
		t.Fatalf("ticket 关联的 challenge = %s, 期望 %s", info.ChallengeID, ch.ID)
	}

	if _, err := c.ConsumeTicket(context.Background(), res.Ticket); err == nil {
		t.Fatal("同一张 ticket 被消费了两次")
	}
}

func TestWrongPositionFails(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{
		ID:       ch.ID,
		X:        answer + 30,
		Duration: 500,
		Trail:    makeTrail(answer+30, 20),
	})
	if res.OK || res.Code != CodeMismatch {
		t.Fatalf("期望 mismatch，实际 %+v", res)
	}

	// 挑战已作废，后续校验应失败。
	res2 := postVerify(t, srv, VerifyRequest{
		ID:       ch.ID,
		X:        answer,
		Duration: 600,
		Trail:    makeTrail(answer, 20),
	})
	if res2.OK || res2.Code != CodeInvalid {
		t.Fatalf("挑战应当一次性作废，实际 %+v", res2)
	}
}

func TestTooFastFails(t *testing.T) {
	_, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.MinSolveDuration = 10 * time.Second
	})
	ch := getChallenge(t, srv)

	res := postVerify(t, srv, VerifyRequest{
		ID:    ch.ID,
		X:     100,
		Trail: makeTrail(100, 20),
	})
	if res.OK || res.Code != CodeTooFast {
		t.Fatalf("期望 too_fast，实际 %+v", res)
	}
}

func TestTrailRejectsTeleport(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{
		ID: ch.ID,
		X:  answer,
		Trail: []TrailPoint{
			{T: 0, X: 0},
			{T: 1, X: answer}, // 1ms 内位移数百像素
		},
	})
	if res.OK || res.Code != CodeTrail {
		t.Fatalf("期望 trail 拦截，实际 %+v", res)
	}
}

func TestExpiredChallenge(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.ChallengeTTL = 50 * time.Millisecond
	})
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)
	time.Sleep(80 * time.Millisecond)

	res := postVerify(t, srv, VerifyRequest{
		ID:    ch.ID,
		X:     answer,
		Trail: makeTrail(answer, 20),
	})
	if res.OK || res.Code != CodeExpired {
		t.Fatalf("期望 expired，实际 %+v", res)
	}
}

func TestPoWRequired(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.PoW = PoWConfig{Enabled: true, Bits: 8}
	})
	ch := getChallenge(t, srv)
	if ch.PoW == nil || ch.PoW.Prefix == "" {
		t.Fatal("没有下发 PoW 题目")
	}
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{
		ID:    ch.ID,
		X:     answer,
		Trail: makeTrail(answer, 20),
	})
	if res.OK || res.Code != CodePoW {
		t.Fatalf("没有 nonce 应当失败，实际 %+v", res)
	}

	ch2 := getChallenge(t, srv)
	answer2 := answerOf(t, c, ch2.ID)
	nonce := solvePoW(ch2.PoW.Prefix, ch2.PoW.Bits)
	res2 := postVerify(t, srv, VerifyRequest{
		ID:    ch2.ID,
		X:     answer2,
		Trail: makeTrail(answer2, 20),
		Nonce: nonce,
	})
	if !res2.OK {
		t.Fatalf("带正确 nonce 应当通过，实际 %+v", res2)
	}
}

func solvePoW(prefix string, bits int) string {
	for i := 0; i < 1<<24; i++ {
		nonce := strconv.Itoa(i)
		if verifyPoW(prefix, bits, nonce) {
			return nonce
		}
	}
	return ""
}

func TestRateLimit(t *testing.T) {
	_, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.RateLimit = RateLimitConfig{Enabled: true, Burst: 2, RefillPerSecond: 0.01, MaxKeys: 100}
	})
	codes := make([]int, 3)
	for i := 0; i < 3; i++ {
		resp, err := srv.Client().Get(srv.URL + "/captcha/challenge")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		resp.Body.Close()
		codes[i] = resp.StatusCode
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("限流结果不对: %v", codes)
	}
}

func TestTicketGuard(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)
	res := postVerify(t, srv, VerifyRequest{ID: ch.ID, X: answer, Trail: makeTrail(answer, 20)})
	if !res.OK {
		t.Fatalf("准备票据失败: %+v", res)
	}

	guarded := c.TicketGuard("")
	h := guarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/action", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无票据应当 403，实际 %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/action", nil)
	req2.RemoteAddr = "127.0.0.1:5555"
	req2.Header.Set("User-Agent", "Go-http-client/1.1")
	req2.Header.Set("X-Captcha-Ticket", res.Ticket)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("有效票据应当放行，实际 %d", rec2.Code)
	}
}

func TestTicketBoundToClient(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)
	res := postVerify(t, srv, VerifyRequest{ID: ch.ID, X: answer, Trail: makeTrail(answer, 20)})
	if !res.OK {
		t.Fatalf("准备票据失败: %+v", res)
	}

	other := httptest.NewRequest(http.MethodPost, "/api/action", nil)
	other.RemoteAddr = "127.0.0.1:6666"
	other.Header.Set("User-Agent", "curl/8.0")
	if _, err := c.ConsumeTicketFor(context.Background(), res.Ticket, other); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("换客户端消费应当失败，实际 err=%v", err)
	}
	// 校验失败也要把凭证作废，防止被重复试。
	if _, err := c.ConsumeTicket(context.Background(), res.Ticket); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("票据应当已经被消费掉，实际 err=%v", err)
	}
}

func postVerifyRaw(srv *httptest.Server, req VerifyRequest) (VerifyResult, error) {
	body, _ := json.Marshal(req)
	resp, err := srv.Client().Post(srv.URL+"/captcha/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		return VerifyResult{}, err
	}
	defer resp.Body.Close()
	var res VerifyResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return VerifyResult{}, err
	}
	return res, nil
}

func TestConcurrentVerifyIssuesSingleTicket(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	const workers = 8
	var okCount int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := postVerifyRaw(srv, VerifyRequest{ID: ch.ID, X: answer, Trail: makeTrail(answer, 12)})
			if err == nil && res.OK {
				atomic.AddInt32(&okCount, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if okCount != 1 {
		t.Fatalf("一个挑战只能签发一张票，实际 %d 张", okCount)
	}
}

func TestRateLimitKeyedByIPNotHeaders(t *testing.T) {
	_, srv := newTestCaptcha(t, func(c *Config) {
		c.RateLimit = RateLimitConfig{Enabled: true, Burst: 2, RefillPerSecond: 0.01, MaxKeys: 100}
	})

	do := func(i int) int {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/captcha/challenge", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("X-Captcha-Client", fmt.Sprintf("rotate-%d", i))
		req.Header.Set("User-Agent", fmt.Sprintf("ua-%d", i))
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := do(0); code != http.StatusOK {
		t.Fatalf("第一次应当 200，实际 %d", code)
	}
	if code := do(1); code != http.StatusOK {
		t.Fatalf("第二次应当 200，实际 %d", code)
	}
	// 换 UA 和设备 ID 不该换限流桶，第三次应该被拦。
	if code := do(2); code != http.StatusTooManyRequests {
		t.Fatalf("换身份不应绕过限流，实际 %d", code)
	}
}

func TestTrailMustStartAtOrigin(t *testing.T) {
	c, srv := newTestCaptcha(t, nil)
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	tr := makeTrail(answer, 10)
	for i := range tr {
		tr[i].X += 40
	}
	tr[len(tr)-1].X = answer
	res := postVerify(t, srv, VerifyRequest{ID: ch.ID, X: answer, Trail: tr})
	if res.OK || res.Code != CodeTrail {
		t.Fatalf("首点不在原点的轨迹应当判 trail，实际 %+v", res)
	}
}

func TestMountCustomPrefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSolveDuration = time.Nanosecond
	cfg.RateLimit.Enabled = false
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	mux := http.NewServeMux()
	c.Mount(mux, "/verify/slider")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/verify/slider/challenge")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("自定义前缀应当可用，实际 %d", resp.StatusCode)
	}

	// 旧前缀不能再访问。
	resp2, err := srv.Client().Get(srv.URL + "/captcha/challenge")
	if err != nil {
		t.Fatalf("GET old: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("旧前缀应当 404，实际 %d", resp2.StatusCode)
	}
}

func TestMountPrefixWithoutLeadingSlash(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSolveDuration = time.Nanosecond
	cfg.RateLimit.Enabled = false
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	mux := http.NewServeMux()
	c.Mount(mux, "captcha") // 前缀缺少前导斜杠
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/captcha/challenge")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("无前导斜杠的前缀应当自动补全，实际 %d", resp.StatusCode)
	}
}

func TestMountAtRoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinSolveDuration = time.Nanosecond
	cfg.RateLimit.Enabled = false
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	mux := http.NewServeMux()
	c.Mount(mux, "/")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/challenge")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("挂到根路径时应当是 /challenge，实际 %d", resp.StatusCode)
	}
}

func TestDefaultBodyLimitFitsMaxTrail(t *testing.T) {
	cfg := DefaultConfig()
	cfg.normalize()
	tr := make([]TrailPoint, cfg.MaxTrailPoints)
	for i := range tr {
		tr[i] = TrailPoint{T: int64(i * 8), X: float64(i) * 0.1234, Y: 0.5}
	}
	body, err := json.Marshal(VerifyRequest{ID: "x", X: 123.45, Duration: 9999, Trail: tr})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if int64(len(body)) >= cfg.MaxBodyBytes {
		t.Fatalf("默认请求体上限 %d 字节装不下 %d 个轨迹点（需要 %d 字节）", cfg.MaxBodyBytes, cfg.MaxTrailPoints, len(body))
	}
}

func makeHumanTrail(dest float64, n int) []TrailPoint {
	if n < 12 {
		n = 12
	}
	tr := make([]TrailPoint, 0, n+1)
	// 起手停顿：按下后 120ms 开始移动。
	tr = append(tr, TrailPoint{T: 0, X: 0, Y: 0})
	t := int64(120)
	for i := 1; i <= n; i++ {
		f := float64(i) / float64(n)
		// 先加速再减速，中间回拉一下。
		ease := f * f * (3 - 2*f)
		x := dest * ease
		if i == n-1 {
			x = dest + 3.2 // 过冲
		}
		if i == n {
			x = dest // 回拉到位
		}
		y := math.Sin(float64(i)*0.7)*1.6 + 0.4
		// 采样间隔不均匀：12~48ms
		dt := int64(12 + (i*7)%37)
		t += dt
		tr = append(tr, TrailPoint{T: t, X: math.Round(x*100) / 100, Y: math.Round(y*100) / 100})
	}
	return tr
}

func TestBehaviorRejectsNaiveBot(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.Behavior.Enabled = true
	})
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	// 匀速直线、等间隔采样、无过冲、Y 恒为 0，属于典型自动化轨迹。
	tr := make([]TrailPoint, 0, 40)
	for i := 0; i < 40; i++ {
		f := float64(i) / 39
		tr = append(tr, TrailPoint{T: int64(i * 16), X: answer * f, Y: 0})
	}
	tr[39].X = answer
	res := postVerify(t, srv, VerifyRequest{
		ID:    ch.ID,
		X:     answer,
		Trail: tr,
		Behavior: &BehaviorSignals{
			PointerType: "mouse",
			Events:      40,
			Trusted:     40,
		},
	})
	if res.OK || res.Code != CodeBehavior {
		t.Fatalf("匀速脚本应当被判 behavior，实际 %+v", res)
	}
	if res.Score >= 45 {
		t.Fatalf("脚本评分不应达到阈值，实际 %d", res.Score)
	}
}

func TestBehaviorAcceptsHumanLikeTrail(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.Behavior.Enabled = true
	})
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{
		ID:       ch.ID,
		X:        answer,
		Trail:    makeHumanTrail(answer, 26),
		Behavior: &BehaviorSignals{PointerType: "mouse", Events: 26, Trusted: 26, Coalesced: 40},
	})
	if !res.OK {
		t.Fatalf("拟人轨迹应通过，实际 %+v", res)
	}
	if res.Score < 45 {
		t.Fatalf("拟人轨迹评分过低：%d", res.Score)
	}
}

func TestBehaviorPenalizesUntrustedEvents(t *testing.T) {
	c := &Captcha{}
	cfg := DefaultConfig()
	cfg.Behavior.Enabled = true
	c.cfg = cfg
	tr := makeHumanTrail(120, 26)
	score, _ := c.scoreBehavior(tr, &BehaviorSignals{PointerType: "mouse", Events: 30, Trusted: 0})
	if score >= cfg.Behavior.MinScore {
		t.Fatalf("伪造事件（isTrusted=false）不应过线，实际 %d", score)
	}
}

func TestSecondGateFlow(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.SecondGate.Enabled = true
	})
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)

	res := postVerify(t, srv, VerifyRequest{ID: ch.ID, X: answer, Trail: makeTrail(answer, 30)})
	if !res.OK || res.Next != "plain" || res.NextID == "" || res.Ticket != "" {
		t.Fatalf("拼图通过后应当返回第二道 plain，实际 %+v", res)
	}

	plainID := res.NextID
	tr := makeTrail(100, 30)
	tr[0].X = 0
	res2 := postVerify(t, srv, VerifyRequest{ID: plainID, X: 100, Trail: tr})
	if !res2.OK || res2.Ticket == "" {
		t.Fatalf("第二道滑块应签发票据，实际 %+v", res2)
	}

	info, err := c.ConsumeTicket(context.Background(), res2.Ticket)
	if err != nil {
		t.Fatalf("ConsumeTicket: %v", err)
	}
	if info.ChallengeID != plainID {
		t.Fatalf("票据应绑定第二道挑战，实际 %s", info.ChallengeID)
	}
}

func TestPlainStageMustReachEnd(t *testing.T) {
	c, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.SecondGate.Enabled = true
	})
	ch := getChallenge(t, srv)
	answer := answerOf(t, c, ch.ID)
	res := postVerify(t, srv, VerifyRequest{ID: ch.ID, X: answer, Trail: makeTrail(answer, 30)})
	if !res.OK || res.NextID == "" {
		t.Fatalf("第一道未通过：%+v", res)
	}
	tr := makeTrail(50, 30)
	tr[0].X = 0
	res2 := postVerify(t, srv, VerifyRequest{ID: res.NextID, X: 50, Trail: tr})
	if res2.OK || res2.Code != CodeMismatch {
		t.Fatalf("没拖到最右不应通过，实际 %+v", res2)
	}
}

type decoyProbeGen struct{ got int }

func (g *decoyProbeGen) Generate(ctx context.Context, req GenRequest) (*GenResult, error) {
	g.got = req.Decoys
	return DefaultGenerator{}.Generate(ctx, req)
}

func TestDecoysWiredToGenerator(t *testing.T) {
	probe := &decoyProbeGen{}
	_, srv := newTestCaptcha(t, func(c *Config) {
		c.Decoys = 1
		c.Generator = probe
	})
	ch := getChallenge(t, srv)
	if probe.got != 1 {
		t.Fatalf("generator got Decoys=%d, want 1", probe.got)
	}
	if ch.Decoys < 0 || ch.Decoys > 1 {
		t.Fatalf("challenge decoys=%d, 应在 0~1 之间（以实际落图为准）", ch.Decoys)
	}
}

func TestDecoysClamped(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Decoys != 3 {
		t.Fatalf("default Decoys=%d, want 3", cfg.Decoys)
	}
	cfg.Decoys = 99
	cfg.normalize()
	if cfg.Decoys != 3 {
		t.Fatalf("normalized Decoys=%d, want 3", cfg.Decoys)
	}
	cfg.Decoys = -1
	cfg.normalize()
	if cfg.Decoys != 0 {
		t.Fatalf("normalized Decoys=%d, want 0", cfg.Decoys)
	}
}

func TestSameRowDecoyPlacement(t *testing.T) {
	const (
		w       = 320
		pw      = 48
		ph      = 48
		pieceX  = 14
		targetX = 200
		targetY = 40
	)
	trueRect := image.Rect(targetX, targetY, targetX+pw, targetY+ph)
	pieceRect := image.Rect(pieceX, targetY, pieceX+pw, targetY+ph)
	x, ok := sameRowDecoyX(w, targetX, targetY, pw, ph, pieceX, 18, 0)
	if !ok {
		t.Fatal("320px 画布应能放下同排伪装")
	}
	if x == 0 {
		t.Fatal("同排伪装 x 不应为 0")
	}
	r := image.Rect(x, targetY, x+pw, targetY+ph)
	if r.Overlaps(trueRect) || r.Overlaps(pieceRect) {
		t.Fatalf("同排伪装与真缺口/碎片重叠: %v", r)
	}
	if r.Min.Y != targetY || r.Max.Y != targetY+ph {
		t.Fatalf("同排伪装 y 不一致: %v", r)
	}
}

func TestSameRowDecoySide(t *testing.T) {
	const (
		w      = 320
		pw     = 48
		ph     = 48
		pieceX = 14
		py     = 40
	)
	cases := []struct {
		side    int
		targetX int
		left    bool
	}{
		{0, 180, true},
		{1, 140, false},
	}
	for _, tc := range cases {
		x, ok := sameRowDecoyX(w, tc.targetX, py, pw, ph, pieceX, 18, tc.side)
		if !ok {
			t.Fatalf("side=%d 放不下同排伪装", tc.side)
		}
		if tc.left && x >= tc.targetX {
			t.Fatalf("side=0 应放在左侧，target=%d decoy=%d", tc.targetX, x)
		}
		if !tc.left && x <= tc.targetX {
			t.Fatalf("side=1 应放在右侧，target=%d decoy=%d", tc.targetX, x)
		}
	}
}

func TestClientIPUsesFirstValidXFF(t *testing.T) {
	c := &Captcha{cfg: DefaultConfig()}
	c.cfg.TrustProxyHeaders = true

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", ", 1.2.3.4, 5.6.7.8")
	if got := c.clientIP(req); got != "1.2.3.4" {
		t.Fatalf("XFF 应取第一个合法 IP，实际 %q", got)
	}

	req.Header.Set("X-Forwarded-For", "not-an-ip")
	req.Header.Set("X-Real-IP", "also-not-an-ip")
	if got := c.clientIP(req); got != "10.0.0.1" {
		t.Fatalf("非法代理头应回退 RemoteAddr，实际 %q", got)
	}
}

func TestCORSPreflightAllowsConfiguredHeaders(t *testing.T) {
	_, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.AllowOrigins = []string{"https://app.example"}
		cfg.AllowHeaders = []string{"Content-Type", "X-Captcha-Client", "X-Custom-Auth"}
	})
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/captcha/challenge", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "x-custom-auth")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("预检状态 = %d，期望 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Custom-Auth") {
		t.Fatalf("Allow-Headers 未包含自定义头: %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("精确 Origin 应允许 credentials，实际 %q", got)
	}
}

func TestMemoryStoreSetAfterClose(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Set(context.Background(), "k", []byte("v"), time.Minute); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("关闭后 Set 应返回 ErrStoreClosed，实际 %v", err)
	}
}

func TestRateLimiterEvictsOldestBucket(t *testing.T) {
	rl := newRateLimiter(RateLimitConfig{Enabled: true, Burst: 1, RefillPerSecond: 0.001, MaxKeys: 2})
	now := time.Now()
	rl.buckets["old"] = &tokenBucket{tokens: 1, last: now.Add(-time.Minute), seen: now.Add(-time.Minute)}
	rl.buckets["new"] = &tokenBucket{tokens: 1, last: now, seen: now}
	if !rl.allow("third") {
		t.Fatal("新 key 应拿到令牌")
	}
	if _, ok := rl.buckets["old"]; ok {
		t.Fatal("应淘汰最久未使用的桶")
	}
	if _, ok := rl.buckets["new"]; !ok {
		t.Fatal("较新的桶不应被淘汰")
	}
	if len(rl.buckets) != 2 {
		t.Fatalf("桶数量 = %d，期望 2", len(rl.buckets))
	}
}

func TestVerifyRejectsTrailingJSON(t *testing.T) {
	_, srv := newTestCaptcha(t, nil)
	body := strings.NewReader(`{"id":"x","x":0}{"id":"y","x":0}`)
	resp, err := srv.Client().Post(srv.URL+"/captcha/verify", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("尾随 JSON 应当 400，实际 %d", resp.StatusCode)
	}
}

type nilGen struct{}

func (nilGen) Generate(context.Context, GenRequest) (*GenResult, error) {
	return nil, nil
}

func TestNilGeneratorDoesNotPanic(t *testing.T) {
	_, srv := newTestCaptcha(t, func(cfg *Config) {
		cfg.Generator = nilGen{}
	})
	resp, err := srv.Client().Get(srv.URL + "/captcha/challenge")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("自定义生成器返回 nil 应当 500，实际 %d", resp.StatusCode)
	}
}

func TestExplicitRateLimitDisabledStaysDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit = RateLimitConfig{Enabled: false}
	cfg.normalize()
	if cfg.RateLimit.Enabled {
		t.Fatal("显式关闭的 RateLimit 被 normalize 改回开启")
	}
}

func TestPoWBitsClampedForFrontend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoW.Enabled = true
	cfg.PoW.Bits = 99
	cfg.normalize()
	if cfg.PoW.Bits != 32 {
		t.Fatalf("PoW.Bits 应夹到 32，实际 %d", cfg.PoW.Bits)
	}
}
