package shanhecaptcha

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math/big"
	mrand "math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	challengeKeyPrefix = "sc:ch:"
	ticketKeyPrefix    = "sc:tk:"
)

// Captcha 是验证码实例。
type Captcha struct {
	cfg   Config
	store Store
	rl    RateLimiter

	ownsStore bool

	mu  sync.Mutex
	rnd *mrand.Rand

	// 限制同时生图的数量，避免占满 CPU。
	gen chan struct{}

	now func() time.Time
}

// New 创建验证码实例。
func New(cfg Config) (*Captcha, error) {
	ownsStore := cfg.Store == nil
	cfg.normalize()

	proxies, err := parseTrustedProxies(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	cfg.trustedProxies = proxies

	seed := time.Now().UnixNano()
	if n, err := rand.Int(rand.Reader, big.NewInt(1<<62)); err == nil {
		seed = n.Int64()
	}

	c := &Captcha{
		cfg:       cfg,
		store:     cfg.Store,
		ownsStore: ownsStore,
		rnd:       mrand.New(mrand.NewSource(seed)),
		now:       time.Now,
	}
	c.rl = cfg.Limiter
	c.gen = make(chan struct{}, cfg.MaxConcurrentGenerations)
	return c, nil
}

// Config 返回当前配置。
func (c *Captcha) Config() Config { return c.cfg }

// Store 返回存储。
func (c *Captcha) Store() Store { return c.store }

// Close 释放资源。只关闭内部创建的存储，外部传入的由调用方自己关。
func (c *Captcha) Close() error {
	if !c.ownsStore {
		return nil
	}
	if m, ok := c.store.(*MemoryStore); ok {
		return m.Close()
	}
	return nil
}

// ClientKey 返回当前请求的客户端标识。
func (c *Captcha) ClientKey(r *http.Request) string {
	ip := c.clientIP(r)
	ipPart := prefixIP(ip, c.cfg.ClientIPPrefixV4, c.cfg.ClientIPPrefixV6)
	sum := sha256.Sum256([]byte(ipPart + "|" + r.UserAgent() + "|" + r.Header.Get("X-Captcha-Client")))
	return hex.EncodeToString(sum[:12])
}

// ipKey 只按 IP 段限流，UA 和设备 ID 都能换。
func (c *Captcha) ipKey(r *http.Request) string {
	return prefixIP(c.clientIP(r), c.cfg.ClientIPPrefixV4, c.cfg.ClientIPPrefixV6)
}

func (c *Captcha) clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}

	if len(c.cfg.trustedProxies) > 0 {
		if !ipInNets(remote, c.cfg.trustedProxies) {
			return remote
		}
		for _, line := range r.Header.Values("X-Forwarded-For") {
			entries := strings.Split(line, ",")
			for i := len(entries) - 1; i >= 0; i-- {
				ip := strings.TrimSpace(entries[i])
				if net.ParseIP(ip) != nil && !ipInNets(ip, c.cfg.trustedProxies) {
					return ip
				}
			}
		}
		return remote
	}

	if c.cfg.TrustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			for _, part := range strings.Split(xff, ",") {
				ip := strings.TrimSpace(part)
				if net.ParseIP(ip) != nil {
					return ip
				}
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" && net.ParseIP(xr) != nil {
			return xr
		}
	}
	return remote
}

func parseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("shanhecaptcha: bad TrustedProxyCIDRs %q", s)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func ipInNets(s string, nets []*net.IPNet) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func prefixIP(ip string, v4bits, v6bits int) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		m := net.CIDRMask(v4bits, 32)
		return v4.Mask(m).String() + "/" + strconv.Itoa(v4bits)
	}
	m := net.CIDRMask(v6bits, 128)
	return parsed.Mask(m).String() + "/" + strconv.Itoa(v6bits)
}

type challengeRecord struct {
	ID           string    `json:"id"`
	Stage        string    `json:"stage,omitempty"` // ""：拼图挑战；"plain"：第二道无图滑块
	AnswerX      float64   `json:"answerX"`         // 拼图：TargetX-PieceX；plain：固定 100（百分比）
	AnswerY      float64   `json:"answerY,omitempty"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	PieceX       int       `json:"pieceX"`
	PieceY       int       `json:"pieceY"`
	PieceW       int       `json:"pieceW"`
	PieceH       int       `json:"pieceH"`
	ClientKey    string    `json:"clientKey"`
	CreatedAt    time.Time `json:"createdAt"`
	AttemptsLeft int       `json:"attemptsLeft"`
	PoWPrefix    string    `json:"powPrefix,omitempty"`
}

func (c *Captcha) createChallenge(r *http.Request) (*Challenge, error) {
	ctx := r.Context()

	select {
	case c.gen <- struct{}{}:
		defer func() { <-c.gen }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	c.mu.Lock()
	seed := c.rnd.Int63()
	c.mu.Unlock()

	gen, err := c.cfg.Generator.Generate(ctx, GenRequest{
		Width:     c.cfg.Width,
		Height:    c.cfg.Height,
		PieceSize: c.cfg.PieceSize,
		Decoys:    c.cfg.Decoys,
		Rand:      mrand.New(mrand.NewSource(seed)),
	})
	if err != nil {
		return nil, err
	}
	if gen == nil || gen.Background == nil || gen.Piece == nil {
		return nil, errors.New("shanhecaptcha: generator returned nil image")
	}
	decoys := gen.Decoys
	if decoys < 0 {
		decoys = 0
	}
	if decoys > 3 {
		decoys = 3
	}

	bgURI, err := jpegDataURI(gen.Background, 82)
	if err != nil {
		return nil, err
	}
	pieceURI, err := pngDataURI(softenPiece(gen.Piece, mrand.New(mrand.NewSource(seed^0x9e3779b9))))
	if err != nil {
		return nil, err
	}

	id, err := randomID(16)
	if err != nil {
		return nil, err
	}
	rec := &challengeRecord{
		ID:           id,
		AnswerX:      float64(gen.TargetX - gen.PieceX),
		Width:        c.cfg.Width,
		Height:       c.cfg.Height,
		PieceX:       gen.PieceX,
		PieceY:       gen.TargetY,
		PieceW:       gen.PieceW,
		PieceH:       gen.PieceH,
		ClientKey:    c.ClientKey(r),
		CreatedAt:    c.now(),
		AttemptsLeft: c.cfg.AttemptsPerChallenge,
	}

	var powQuestion *PoWQuestion
	if c.cfg.PoW.Enabled {
		prefix, err := randomID(8)
		if err != nil {
			return nil, err
		}
		rec.PoWPrefix = prefix
		powQuestion = &PoWQuestion{Algo: "sha256", Prefix: prefix, Bits: c.cfg.PoW.Bits}
	}

	if err := c.put(ctx, challengeKeyPrefix+id, rec, c.cfg.ChallengeTTL+time.Minute); err != nil {
		return nil, err
	}

	c.emit(Event{Kind: "challenge", ChallengeID: id, ClientKey: rec.ClientKey, At: rec.CreatedAt})

	return &Challenge{
		ID:          id,
		Mode:        "puzzle",
		Background:  bgURI,
		Piece:       pieceURI,
		Width:       gen.Background.Rect.Dx(),
		Height:      gen.Background.Rect.Dy(),
		PieceX:      gen.PieceX,
		PieceY:      gen.TargetY,
		PieceWidth:  gen.PieceW,
		PieceHeight: gen.PieceH,
		Decoys:      decoys,
		Tolerance:   c.cfg.Tolerance,
		ExpiresIn:   int(c.cfg.ChallengeTTL.Seconds()),
		PoW:         powQuestion,
	}, nil
}

// 第二道没有图片，只需要把滑块拖到最右。
func (c *Captcha) newPlainChallenge(ctx context.Context, parent *challengeRecord) (string, error) {
	id, err := randomID(16)
	if err != nil {
		return "", err
	}
	rec := &challengeRecord{
		ID:           id,
		Stage:        "plain",
		AnswerX:      100,
		ClientKey:    parent.ClientKey,
		CreatedAt:    c.now(),
		AttemptsLeft: c.cfg.AttemptsPerChallenge,
	}
	if err := c.put(ctx, challengeKeyPrefix+id, rec, c.cfg.ChallengeTTL+time.Minute); err != nil {
		return "", err
	}
	c.emit(Event{Kind: "challenge", ChallengeID: id, ClientKey: rec.ClientKey, At: rec.CreatedAt})
	return id, nil
}

type ticketRecord struct {
	ID          string    `json:"id"`
	ChallengeID string    `json:"challengeId"`
	ClientKey   string    `json:"clientKey"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// ConsumeTicket 消费一次性凭证；同一张只能用一次。
// 不检查客户端，需要绑定就用 ConsumeTicketFor。
func (c *Captcha) ConsumeTicket(ctx context.Context, ticket string) (*TicketInfo, error) {
	return c.consumeTicket(ctx, ticket, "")
}

// ConsumeTicketFor 消费凭证并检查客户端；检查失败也会作废。
func (c *Captcha) ConsumeTicketFor(ctx context.Context, ticket string, r *http.Request) (*TicketInfo, error) {
	return c.consumeTicket(ctx, ticket, c.ClientKey(r))
}

func (c *Captcha) consumeTicket(ctx context.Context, ticket, clientKey string) (*TicketInfo, error) {
	if ticket == "" {
		return nil, ErrTicketInvalid
	}
	raw, err := c.take(ctx, ticketKeyPrefix+ticket)
	if err != nil {
		return nil, ErrTicketInvalid
	}
	var rec ticketRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, ErrTicketInvalid
	}
	if c.cfg.BindToClient && clientKey != "" && rec.ClientKey != clientKey {
		return nil, ErrTicketInvalid
	}
	if c.now().After(rec.ExpiresAt) {
		return nil, ErrTicketExpired
	}
	return &TicketInfo{
		ID:          rec.ID,
		ChallengeID: rec.ChallengeID,
		ClientKey:   rec.ClientKey,
		CreatedAt:   rec.CreatedAt,
		ExpiresAt:   rec.ExpiresAt,
	}, nil
}

func (c *Captcha) issueTicket(ctx context.Context, ch *challengeRecord) (string, error) {
	id, err := randomID(16)
	if err != nil {
		return "", err
	}
	now := c.now()
	rec := &ticketRecord{
		ID:          id,
		ChallengeID: ch.ID,
		ClientKey:   ch.ClientKey,
		CreatedAt:   now,
		ExpiresAt:   now.Add(c.cfg.TicketTTL),
	}
	if err := c.put(ctx, ticketKeyPrefix+id, rec, c.cfg.TicketTTL); err != nil {
		return "", err
	}
	return id, nil
}

type ticketCtxKey struct{}

// TicketGuard 检查请求头里的凭证；没有或无效就返回 403，同时核对 X-Captcha-Client。
func (c *Captcha) TicketGuard(headerName string) func(http.Handler) http.Handler {
	if headerName == "" {
		headerName = "X-Captcha-Ticket"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, err := c.ConsumeTicketFor(r.Context(), r.Header.Get(headerName), r)
			if err != nil {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "captcha_required"})
				return
			}
			ctx := context.WithValue(r.Context(), ticketCtxKey{}, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TicketFromContext 从 context 取凭证信息。
func TicketFromContext(ctx context.Context) (*TicketInfo, bool) {
	info, ok := ctx.Value(ticketCtxKey{}).(*TicketInfo)
	return info, ok
}

func (c *Captcha) put(ctx context.Context, key string, v any, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, key, data, ttl)
}

func (c *Captcha) take(ctx context.Context, key string) ([]byte, error) {
	if t, ok := c.store.(TakeStore); ok {
		return t.Take(ctx, key)
	}
	data, err := c.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = c.store.Delete(ctx, key)
	return data, nil
}

func (c *Captcha) emit(e Event) {
	if c.cfg.OnEvent != nil {
		c.cfg.OnEvent(e)
	}
}

var randomIDFailed = errors.New("shanhecaptcha: crypto/rand unavailable")

func randomID(nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", randomIDFailed
	}
	return hex.EncodeToString(b), nil
}

func jpegDataURI(img image.Image, quality int) (string, error) {
	var sb strings.Builder
	sb.WriteString("data:image/jpeg;base64,")
	enc := base64.NewEncoder(base64.StdEncoding, &sb)
	if err := jpeg.Encode(enc, img, &jpeg.Options{Quality: quality}); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func pngDataURI(img image.Image) (string, error) {
	var sb strings.Builder
	sb.WriteString("data:image/png;base64,")
	enc := base64.NewEncoder(base64.StdEncoding, &sb)
	e := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := e.Encode(enc, img); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func verifyPoW(prefix string, bits int, nonce string) bool {
	if bits <= 0 {
		return true
	}
	if len(nonce) > 64 {
		return false
	}
	sum := sha256.Sum256([]byte(prefix + nonce))
	zeroBits := 0
	for _, b := range sum {
		if b == 0 {
			zeroBits += 8
			continue
		}
		for i := 7; i >= 0; i-- {
			if b&(1<<uint(i)) == 0 {
				zeroBits++
			} else {
				break
			}
		}
		break
	}
	return zeroBits >= bits
}
