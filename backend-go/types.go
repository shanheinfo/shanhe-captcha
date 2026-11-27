package shanhecaptcha

import "time"

// Event 是验证码运行中发出的事件。Kind 有 challenge、success、failure、rate_limited。
type Event struct {
	Kind        string
	ChallengeID string
	ClientKey   string
	Reason      string
	IP          string
	Elapsed     time.Duration
	At          time.Time
}

// Challenge 是发给前端的数据，里面没有缺口答案。
type Challenge struct {
	ID          string       `json:"id"`
	Background  string       `json:"background"`
	Piece       string       `json:"piece"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	PieceX      int          `json:"pieceX"`
	PieceY      int          `json:"pieceY"`
	PieceWidth  int          `json:"pieceWidth"`
	PieceHeight int          `json:"pieceHeight"`
	Decoys      int          `json:"decoys,omitempty"`
	Mode        string       `json:"mode"`
	Tolerance   int          `json:"tolerance"`
	ExpiresIn   int          `json:"expiresIn"`
	PoW         *PoWQuestion `json:"pow,omitempty"`
}

// PoWQuestion 是可选的小计算题：找一个 nonce，
// 让 sha256(prefix + nonce) 的结果开头有足够多个 0 位。
type PoWQuestion struct {
	Algo   string `json:"algo"`
	Prefix string `json:"prefix"`
	Bits   int    `json:"bits"`
}

// TrailPoint 是轨迹点。t 是毫秒，x/y 是相对拖动起点的图片像素。
type TrailPoint struct {
	T int64   `json:"t"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	P float64 `json:"p,omitempty"`
}

type VerifyRequest struct {
	ID       string           `json:"id"`
	X        float64          `json:"x"`
	Y        float64          `json:"y,omitempty"`
	Duration int64            `json:"duration"`
	Trail    []TrailPoint     `json:"trail,omitempty"`
	Nonce    string           `json:"nonce,omitempty"`
	Behavior *BehaviorSignals `json:"behavior,omitempty"`
}

type VerifyResult struct {
	OK     bool   `json:"ok"`
	Ticket string `json:"ticket,omitempty"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	Retry  bool   `json:"retry"`
	Score  int    `json:"score,omitempty"`
	// 第二道验证用。next="plain" 时，前端拿 NextID 再提交一次。
	Next   string `json:"next,omitempty"`
	NextID string `json:"nextId,omitempty"`
}

type TicketInfo struct {
	ID          string
	ChallengeID string
	ClientKey   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}
