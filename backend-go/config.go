package shanhecaptcha

import (
	"log/slog"
	"net"
	"runtime"
	"strings"
	"time"
)

// 按 IP 段限制请求频率。
type RateLimitConfig struct {
	Enabled         bool
	Burst           int
	RefillPerSecond float64
	MaxKeys         int
}

// SecondGateConfig 控制拼图通过后的第二道无图滑块。
type SecondGateConfig struct {
	Enabled bool
}

// PoWConfig 是可选的工作量证明。
type PoWConfig struct {
	Enabled bool
	Bits    int
}

// 直接 New(Config{}) 时校验开关都是关的，从 DefaultConfig() 改。
type Config struct {
	Prefix string

	Width     int
	Height    int
	PieceSize int

	// Decoys 是假缺口数量，默认 3，最大 3；0 关闭。
	// 第一个假缺口优先与真缺口同排，其余随机放。
	Decoys    int
	Tolerance int

	ChallengeTTL     time.Duration
	TicketTTL        time.Duration
	MinSolveDuration time.Duration

	AttemptsPerChallenge int

	BindToClient      bool
	ClientIPPrefixV4  int
	ClientIPPrefixV6  int
	TrustProxyHeaders bool
	// TrustedProxyCIDRs 非空时只信任这些网段发来的 X-Forwarded-For，
	// 从右往左取第一个不在网段内的 IP，此时 TrustProxyHeaders 不生效。
	TrustedProxyCIDRs []string

	VerifyTrail     bool
	MinTrailPoints  int
	MaxTrailPoints  int
	MaxSpeedPxPerMs float64

	MaxBodyBytes int64

	MaxConcurrentGenerations int

	AllowOrigins []string
	AllowHeaders []string

	RateLimit  RateLimitConfig
	PoW        PoWConfig
	Behavior   BehaviorConfig
	SecondGate SecondGateConfig

	Store     Store
	Generator Generator

	Logger  *slog.Logger
	Debug   bool
	OnEvent func(Event)

	trustedProxies []*net.IPNet
}

func DefaultConfig() Config {
	return Config{
		Prefix:               "/captcha",
		Width:                320,
		Height:               160,
		PieceSize:            48,
		Decoys:               3,
		Tolerance:            4,
		ChallengeTTL:         90 * time.Second,
		TicketTTL:            60 * time.Second,
		MinSolveDuration:     250 * time.Millisecond,
		AttemptsPerChallenge: 1,
		BindToClient:         true,
		ClientIPPrefixV4:     24,
		ClientIPPrefixV6:     64,
		VerifyTrail:          true,
		MinTrailPoints:       8,
		MaxTrailPoints:       3000,
		MaxSpeedPxPerMs:      10,
		MaxBodyBytes:         128 << 10,
		RateLimit: RateLimitConfig{
			Enabled:         true,
			Burst:           8,
			RefillPerSecond: 0.5,
			MaxKeys:         10000,
		},
		PoW:      PoWConfig{Enabled: true, Bits: 18},
		Behavior: BehaviorConfig{Enabled: true, MinScore: 45},
	}
}

func (c *Config) normalize() {
	if c.Prefix == "" {
		c.Prefix = "/captcha"
	}
	if !strings.HasPrefix(c.Prefix, "/") {
		c.Prefix = "/" + c.Prefix
	}
	if c.Width <= 0 {
		c.Width = 320
	}
	if c.Height <= 0 {
		c.Height = 160
	}
	if c.PieceSize <= 0 {
		c.PieceSize = 48
	}
	if c.PieceSize > c.Height-16 {
		c.PieceSize = c.Height - 16
	}
	if c.Decoys < 0 {
		c.Decoys = 0
	}
	if c.Decoys > 3 {
		c.Decoys = 3
	}
	if c.Tolerance < 0 {
		c.Tolerance = 0
	}
	if c.ChallengeTTL <= 0 {
		c.ChallengeTTL = 90 * time.Second
	}
	if c.TicketTTL <= 0 {
		c.TicketTTL = 60 * time.Second
	}
	if c.MinSolveDuration <= 0 {
		c.MinSolveDuration = 250 * time.Millisecond
	}
	if c.AttemptsPerChallenge <= 0 {
		c.AttemptsPerChallenge = 1
	}
	if c.ClientIPPrefixV4 <= 0 || c.ClientIPPrefixV4 > 32 {
		c.ClientIPPrefixV4 = 24
	}
	if c.ClientIPPrefixV6 <= 0 || c.ClientIPPrefixV6 > 128 {
		c.ClientIPPrefixV6 = 64
	}
	if c.MinTrailPoints <= 0 {
		c.MinTrailPoints = 8
	}
	if c.MaxTrailPoints <= 0 {
		c.MaxTrailPoints = 3000
	}
	if c.MaxSpeedPxPerMs <= 0 {
		c.MaxSpeedPxPerMs = 10
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 128 << 10
	}
	if c.Behavior.MinScore <= 0 || c.Behavior.MinScore > 100 {
		c.Behavior.MinScore = 45
	}
	if c.MaxConcurrentGenerations <= 0 {
		c.MaxConcurrentGenerations = runtime.GOMAXPROCS(0)
		if c.MaxConcurrentGenerations < 4 {
			c.MaxConcurrentGenerations = 4
		}
	}
	if c.RateLimit.Enabled && c.RateLimit.Burst == 0 && c.RateLimit.RefillPerSecond == 0 && c.RateLimit.MaxKeys == 0 {
		c.RateLimit = DefaultConfig().RateLimit
	}
	if c.RateLimit.Burst <= 0 {
		c.RateLimit.Burst = 8
	}
	if c.RateLimit.RefillPerSecond <= 0 {
		c.RateLimit.RefillPerSecond = 0.5
	}
	if c.RateLimit.MaxKeys <= 0 {
		c.RateLimit.MaxKeys = 10000
	}
	if c.PoW.Enabled && c.PoW.Bits <= 0 {
		c.PoW.Bits = 18
	}
	if c.PoW.Bits > 32 {
		// 前端求解器按 32 位前导零实现，更高的位数无法有效求解。
		c.PoW.Bits = 32
	}
	if c.Store == nil {
		c.Store = NewMemoryStore()
	}
	if c.Generator == nil {
		c.Generator = DefaultGenerator{}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}
