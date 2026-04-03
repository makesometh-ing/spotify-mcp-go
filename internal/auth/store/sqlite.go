package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteTokenStore persists token records to a SQLite database.
type SQLiteTokenStore struct {
	db *sql.DB
}

// NewSQLiteTokenStore opens (or creates) a SQLite database at path and
// initialises the tokens table.
func NewSQLiteTokenStore(path string) (*SQLiteTokenStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating table: %w", err)
	}

	return &SQLiteTokenStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *SQLiteTokenStore) Close() error {
	return s.db.Close()
}

const createTableSQL = `CREATE TABLE IF NOT EXISTS tokens (
	client_id            TEXT PRIMARY KEY,
	spotify_access_token TEXT NOT NULL DEFAULT '',
	spotify_refresh_token TEXT NOT NULL DEFAULT '',
	spotify_token_expiry TEXT NOT NULL DEFAULT '',
	mcp_access_token     TEXT NOT NULL DEFAULT '',
	mcp_refresh_token    TEXT NOT NULL DEFAULT '',
	mcp_token_expiry     TEXT NOT NULL DEFAULT '',
	created_at           TEXT NOT NULL DEFAULT ''
)`

func (s *SQLiteTokenStore) Store(_ context.Context, clientID string, tokens *TokenRecord) error {
	_, err := s.db.Exec(`INSERT INTO tokens
		(client_id, spotify_access_token, spotify_refresh_token, spotify_token_expiry,
		 mcp_access_token, mcp_refresh_token, mcp_token_expiry, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id) DO UPDATE SET
			spotify_access_token=excluded.spotify_access_token,
			spotify_refresh_token=excluded.spotify_refresh_token,
			spotify_token_expiry=excluded.spotify_token_expiry,
			mcp_access_token=excluded.mcp_access_token,
			mcp_refresh_token=excluded.mcp_refresh_token,
			mcp_token_expiry=excluded.mcp_token_expiry,
			created_at=excluded.created_at`,
		clientID,
		tokens.SpotifyAccessToken,
		tokens.SpotifyRefreshToken,
		tokens.SpotifyTokenExpiry.UTC().Format(time.RFC3339),
		tokens.MCPAccessToken,
		tokens.MCPRefreshToken,
		tokens.MCPTokenExpiry.UTC().Format(time.RFC3339),
		tokens.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteTokenStore) Load(_ context.Context, clientID string) (*TokenRecord, error) {
	var (
		spotifyExpiry, mcpExpiry, createdAt string
		r                                  TokenRecord
	)
	err := s.db.QueryRow(`SELECT
		spotify_access_token, spotify_refresh_token, spotify_token_expiry,
		mcp_access_token, mcp_refresh_token, mcp_token_expiry, created_at
		FROM tokens WHERE client_id = ?`, clientID).Scan(
		&r.SpotifyAccessToken, &r.SpotifyRefreshToken, &spotifyExpiry,
		&r.MCPAccessToken, &r.MCPRefreshToken, &mcpExpiry, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.SpotifyTokenExpiry, _ = time.Parse(time.RFC3339, spotifyExpiry)
	r.MCPTokenExpiry, _ = time.Parse(time.RFC3339, mcpExpiry)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &r, nil
}

func (s *SQLiteTokenStore) Delete(_ context.Context, clientID string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE client_id = ?`, clientID)
	return err
}

// CleanupExpired removes token records older than ttl and returns the count removed.
func (s *SQLiteTokenStore) CleanupExpired(_ context.Context, ttl time.Duration) (int64, error) {
	cutoff := time.Now().Add(-ttl).UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`DELETE FROM tokens WHERE created_at < ? AND created_at != ''`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
