package store

import (
	"context"

	"go.uber.org/zap"
)

// LoggingTokenStore wraps a TokenStore and logs every operation.
type LoggingTokenStore struct {
	inner  TokenStore
	logger *zap.SugaredLogger
}

// NewLoggingTokenStore returns a TokenStore that logs store, load, and delete operations.
func NewLoggingTokenStore(inner TokenStore, logger *zap.SugaredLogger) *LoggingTokenStore {
	return &LoggingTokenStore{
		inner:  inner,
		logger: logger.Named("store"),
	}
}

func (s *LoggingTokenStore) Store(ctx context.Context, clientID string, tokens *TokenRecord) error {
	err := s.inner.Store(ctx, clientID, tokens)
	s.logger.Debugw("token store: store", "client_id", clientID, "error", err)
	return err
}

func (s *LoggingTokenStore) Load(ctx context.Context, clientID string) (*TokenRecord, error) {
	record, err := s.inner.Load(ctx, clientID)
	found := record != nil
	s.logger.Debugw("token store: load", "client_id", clientID, "found", found, "error", err)
	return record, err
}

func (s *LoggingTokenStore) Delete(ctx context.Context, clientID string) error {
	err := s.inner.Delete(ctx, clientID)
	s.logger.Debugw("token store: delete", "client_id", clientID, "error", err)
	return err
}
