// Package shanhecaptcha 提供可挂载到 net/http 的滑块验证码服务端。
// 挑战与票据由服务端生成和校验，支持轨迹校验、限流、PoW、行为评分及第二道无图滑块。
package shanhecaptcha

import "errors"

const Version = "0.1.2"

var (
	ErrTicketInvalid  = errors.New("shanhecaptcha: ticket invalid")
	ErrTicketExpired  = errors.New("shanhecaptcha: ticket expired")
	ErrTicketConsumed = errors.New("shanhecaptcha: ticket already consumed")
	ErrStoreNotFound  = errors.New("shanhecaptcha: key not found")
	ErrStoreClosed    = errors.New("shanhecaptcha: store closed")
)

const (
	CodeInvalid        = "invalid"
	CodeExpired        = "expired"
	CodeMismatch       = "mismatch"
	CodeTooFast        = "too_fast"
	CodeTrail          = "trail"
	CodeBehavior       = "behavior"
	CodePoW            = "pow"
	CodeClientMismatch = "client_mismatch"
	CodeRateLimited    = "rate_limited"
	CodeBadRequest     = "bad_request"
)
