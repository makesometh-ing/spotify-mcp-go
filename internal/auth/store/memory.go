package store

import (
	"context"
	"sync"
)

// InMemoryTokenStore is a development/testing TokenStore backed by a map.
// Tokens are lost on process restart.
type InMemoryTokenStore struct {
	mu      sync.RWMutex
	records map[string]*TokenRecord
}

// NewInMemoryTokenStore returns a ready-to-use in-memory token store.
func NewInMemoryTokenStore() *InMemoryTokenStore {
	return &InMemoryTokenStore{
		records: make(map[string]*TokenRecord),
	}
}

func (s *InMemoryTokenStore) Store(_ context.Context, clientID string, tokens *TokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[clientID] = tokens
	return nil
}

func (s *InMemoryTokenStore) Load(_ context.Context, clientID string) (*TokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[clientID]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (s *InMemoryTokenStore) Delete(_ context.Context, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, clientID)
	return nil
}
