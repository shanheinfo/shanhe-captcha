package shanhecaptcha

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Handler 返回挂载在 Prefix 下的处理器，包含挑战和校验两个接口。
func (c *Captcha) Handler() http.Handler {
	return c.handlerAt(c.cfg.Prefix)
}

// handlerAt 按指定前缀生成处理器。
func (c *Captcha) handlerAt(prefix string) http.Handler {
	base := prefixBase(prefix)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if c.applyCORS(w, r) {
			return
		}

		switch r.URL.Path {
		case base + "/challenge":
			c.handleChallenge(w, r)
		case base + "/verify":
			c.handleVerify(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// Mount 把处理器注册到 ServeMux。
func (c *Captcha) Mount(mux *http.ServeMux, prefix string) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = c.cfg.Prefix
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if strings.Trim(prefix, "/") == "" {
		mux.Handle("/", c.handlerAt("/"))
		return
	}
	mux.Handle(strings.TrimRight(prefix, "/")+"/", c.handlerAt(prefix))
}

// prefixBase 去掉前缀结尾的斜杠。
func prefixBase(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

func (c *Captcha) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}

	key := c.ClientKey(r)
	if !c.rl.Allow("ch:" + c.ipKey(r)) {
		c.emit(Event{Kind: "rate_limited", ClientKey: key, IP: c.clientIP(r), At: c.now()})
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": CodeRateLimited})
		return
	}

	ch, err := c.createChallenge(r)
	if err != nil {
		c.cfg.Logger.Error("shanhecaptcha: generate challenge failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ch)
}

func (c *Captcha) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}

	key := c.ClientKey(r)
	if !c.rl.Allow("vf:" + c.ipKey(r)) {
		c.emit(Event{Kind: "rate_limited", ClientKey: key, IP: c.clientIP(r), At: c.now()})
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": CodeRateLimited})
		return
	}

	body := http.MaxBytesReader(w, r.Body, c.cfg.MaxBodyBytes)
	defer body.Close()

	var req VerifyRequest
	dec := json.NewDecoder(body)
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": CodeBadRequest})
		return
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": CodeBadRequest})
		return
	}

	res := c.verify(r.Context(), r, &req)
	if c.cfg.Debug && !res.OK {
		res.Reason = res.Code
	}
	if !res.OK {
		res.Retry = true
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (c *Captcha) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	if len(c.cfg.AllowOrigins) == 0 {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	allowed := ""
	for _, o := range c.cfg.AllowOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			allowed = o
			break
		}
	}
	if allowed == "" {
		return false
	}
	if allowed == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", c.corsAllowHeaders())
	w.Header().Set("Access-Control-Max-Age", "600")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (c *Captcha) corsAllowHeaders() string {
	headers := c.cfg.AllowHeaders
	if len(headers) == 0 {
		headers = []string{"Content-Type", "X-Captcha-Client", "Authorization"}
	}
	return strings.Join(headers, ", ")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
