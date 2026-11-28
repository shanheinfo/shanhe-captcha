package shanhecaptcha

import (
	"context"
	"sync"
	"time"
)

// MemoryStore 是进程内存储，适用于单实例和本地开发；进程重启后数据会丢失，多实例应使用共享存储。
type MemoryStore struct {
	mu     sync.RWMutex
	items  map[string]*memItem
	stop   chan struct{}
	closed bool
}

type memItem struct {
	value   []byte
	expires time.Time
}

func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		items: make(map[string]*memItem),
		stop:  make(chan struct{}),
	}
	go s.janitor(30 * time.Second)
	return s
}

func (s *MemoryStore) janitor(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			s.mu.Lock()
			for k, it := range s.items {
				if !it.expires.IsZero() && now.After(it.expires) {
					delete(s.items, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	it, ok := s.items[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrStoreNotFound
	}
	if !it.expires.IsZero() && time.Now().After(it.expires) {
		s.mu.Lock()
		delete(s.items, key)
		s.mu.Unlock()
		return nil, ErrStoreNotFound
	}
	cp := make([]byte, len(it.value))
	copy(cp, it.value)
	return cp, nil
}

func (s *MemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	cp := make([]byte, len(value))
	copy(cp, value)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	s.items[key] = &memItem{value: cp, expires: exp}
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Take(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return nil, ErrStoreNotFound
	}
	delete(s.items, key)
	if !it.expires.IsZero() && time.Now().After(it.expires) {
		return nil, ErrStoreNotFound
	}
	return it.value, nil
}

func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	s.closed = true
	close(s.stop)
	s.items = make(map[string]*memItem)
	return nil
}
