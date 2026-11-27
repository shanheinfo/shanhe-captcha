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
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	rl    *rateLimiter

	mu  sync.Mutex
	rnd *mrand.Rand

	// 限制同时生图的数量，避免占满 CPU。
	gen chan struct{}

	now func() time.Time
}

// New 创建验证码实例。
func New(cfg Config) (*Captcha, error) {
	cfg.normalize()

	seed := time.Now().UnixNano()
	if n, err := rand.Int(rand.Reader, big.NewInt(1<<62)); err == nil {
		seed = n.Int64()
	}

	c := &Captcha{
		cfg:   cfg,
		store: cfg.Store,
		rnd:   mrand.New(mrand.NewSource(seed)),
		now:   time.Now,
	}
	c.rl = newRateLimiter(cfg.RateLimit)
	c.gen = make(chan struct{}, cfg.MaxConcurrentGenerations)
	return c, nil
}

// Config 返回当前配置。
func (c *Captcha) Config() Config { return c.cfg }

// Store 返回存储。
func (c *Captcha) Store() Store { return c.store }

// Close 释放资源。
func (c *Captcha) Close() error {
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

// ipKey 只按 IP 段生成限流 key。
// 不能使用 ClientKey，UA 和设备 ID 都可以随便换。
func (c *Captcha) ipKey(r *http.Request) string {
	return prefixIP(c.clientIP(r), c.cfg.ClientIPPrefixV4, c.cfg.ClientIPPrefixV6)
}

func (c *Captcha) clientIP(r *http.Request) string {
	if c.cfg.TrustProxyHeaders {
		// X-Forwarded-For 只取第一个合法 IP。
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
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
	pieceURI, err := pngDataURI(gen.Piece)
	if err != nil {
		return nil, err
	}

	id := randomID(16)
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
		rec.PoWPrefix = randomID(8)
		powQuestion = &PoWQuestion{Algo: "sha256", Prefix: rec.PoWPrefix, Bits: c.cfg.PoW.Bits}
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
	id := randomID(16)
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
	id := randomID(16)
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

var idSeq uint64

func randomID(nbytes int) string {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败时，用时间、序号和 PID 混合生成。
		seq := atomic.AddUint64(&idSeq, 1)
		h := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d", time.Now().UnixNano(), seq, os.Getpid())))
		if nbytes <= len(h) {
			return hex.EncodeToString(h[:nbytes])
		}
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
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
