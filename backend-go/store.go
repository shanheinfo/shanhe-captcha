package shanhecaptcha

import (
	"context"
	"time"
)

// Store 是挑战与票据的存储接口；Get 未命中返回 ErrStoreNotFound，TakeStore 可选。
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

type TakeStore interface {
	Take(ctx context.Context, key string) ([]byte, error)
}
