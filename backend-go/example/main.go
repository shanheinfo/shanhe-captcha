// Command example 启动演示服务：验证码接口、演示站和登录接口。
// 先进入 backend-go，执行 go run ./example，然后打开 http://127.0.0.1:8090。
// 演示站文件在 ../frontend/demo-dist，改前端后先执行 npm run build:demo。
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	shanhecaptcha "github.com/shanheinfo/shanhe-captcha/backend-go"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "监听地址")
	dist := flag.String("dist", "../frontend/demo-dist", "演示站静态目录")
	flag.Parse()

	cfg := shanhecaptcha.DefaultConfig()
	cfg.Debug = true
	cfg.AttemptsPerChallenge = 1
	cfg.SecondGate.Enabled = true
	cfg.RateLimit = shanhecaptcha.RateLimitConfig{Enabled: true, Burst: 30, RefillPerSecond: 1, MaxKeys: 10000}
	cfg.OnEvent = func(e shanhecaptcha.Event) {
		switch e.Kind {
		case "success":
			log.Printf("[captcha] success challenge=%s elapsed=%s", e.ChallengeID, e.Elapsed.Round(time.Millisecond))
		case "failure":
			log.Printf("[captcha] failure challenge=%s code=%s elapsed=%s", e.ChallengeID, e.Reason, e.Elapsed.Round(time.Millisecond))
		case "rate_limited":
			log.Printf("[captcha] rate limited ip=%s", e.IP)
		}
	}

	c, err := shanhecaptcha.New(cfg)
	if err != nil {
		log.Fatalf("create captcha: %v", err)
	}
	defer c.Close()

	mux := http.NewServeMux()
	c.Mount(mux, "/captcha")

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "message": "method not allowed"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)

		var body struct {
			Ticket string `json:"ticket"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求格式不对"})
			return
		}

		// 执行业务动作前先消费一次性票据。ConsumeTicketFor 会校验票据与当前客户端是否一致。
		info, err := c.ConsumeTicketFor(r.Context(), body.Ticket, r)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "票据无效、已过期或已被使用"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"message":     "人机验证通过，已登录（演示）",
			"challengeId": info.ChallengeID,
			"clientKey":   info.ClientKey,
			"expiresAt":   info.ExpiresAt.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": shanhecaptcha.Version})
	})

	if _, err := os.Stat(*dist); err != nil {
		log.Printf("静态目录 %q 不存在，先执行 cd ../frontend && npm run build:demo", *dist)
	}
	mux.Handle("/", http.FileServer(http.Dir(*dist)))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("demo on http://%s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
