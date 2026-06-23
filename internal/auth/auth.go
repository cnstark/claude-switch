package auth

import (
	"crypto/subtle"
	"sync"
)

// Store 管理私有 key → 项目名的映射，支持并发安全的热重载。
type Store struct {
	mu   sync.RWMutex
	keys map[string]string
}

// NewStore 创建鉴权存储。keys 不可为 nil。
func NewStore(keys map[string]string) *Store {
	if keys == nil {
		keys = make(map[string]string)
	}
	return &Store{keys: keys}
}

// Authenticate 恒定时间比较，返回项目名和是否成功。并发安全。
func (s *Store) Authenticate(apiKey string) (string, bool) {
	if apiKey == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, proj := range s.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) == 1 {
			return proj, true
		}
	}
	return "", false
}

// Update 并发安全地替换 key 表（热重载用）。nil map 被拒绝。
func (s *Store) Update(keys map[string]string) {
	if keys == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}
