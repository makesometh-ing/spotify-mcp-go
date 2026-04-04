package store

import (
	"context"
	"database/sql"
	"encoding/json"
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

	for _, stmt := range migrateSQL {
		_, _ = db.Exec(stmt)
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

// migrateSQL adds columns introduced after the initial schema.
// Each statement uses IF NOT EXISTS / ignores errors so it is safe to re-run.
var migrateSQL = []string{
	`ALTER TABLE tokens ADD COLUMN redirect_uris TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE tokens ADD COLUMN grant_types TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE tokens ADD COLUMN response_types TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE tokens ADD COLUMN token_endpoint_auth_method TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE tokens ADD COLUMN client_name TEXT NOT NULL DEFAULT ''`,
}

func (s *SQLiteTokenStore) Store(_ context.Context, clientID string, tokens *TokenRecord) error {
	redirectURIs, _ := json.Marshal(tokens.RedirectURIs)
	grantTypes, _ := json.Marshal(tokens.GrantTypes)
	responseTypes, _ := json.Marshal(tokens.ResponseTypes)

	_, err := s.db.Exec(`INSERT INTO tokens
		(client_id, spotify_access_token, spotify_refresh_token, spotify_token_expiry,
		 mcp_access_token, mcp_refresh_token, mcp_token_expiry, created_at,
		 redirect_uris, grant_types, response_types, token_endpoint_auth_method, client_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id) DO UPDATE SET
			spotify_access_token=excluded.spotify_access_token,
			spotify_refresh_token=excluded.spotify_refresh_token,
			spotify_token_expiry=excluded.spotify_token_expiry,
			mcp_access_token=excluded.mcp_access_token,
			mcp_refresh_token=excluded.mcp_refresh_token,
			mcp_token_expiry=excluded.mcp_token_expiry,
			created_at=excluded.created_at,
			redirect_uris=excluded.redirect_uris,
			grant_types=excluded.grant_types,
			response_types=excluded.response_types,
			token_endpoint_auth_method=excluded.token_endpoint_auth_method,
			client_name=excluded.client_name`,
		clientID,
		tokens.SpotifyAccessToken,
		tokens.SpotifyRefreshToken,
		tokens.SpotifyTokenExpiry.UTC().Format(time.RFC3339),
		tokens.MCPAccessToken,
		tokens.MCPRefreshToken,
		tokens.MCPTokenExpiry.UTC().Format(time.RFC3339),
		tokens.CreatedAt.UTC().Format(time.RFC3339),
		string(redirectURIs),
		string(grantTypes),
		string(responseTypes),
		tokens.TokenEndpointAuthMethod,
		tokens.ClientName,
	)
	return err
}

func (s *SQLiteTokenStore) Load(_ context.Context, clientID string) (*TokenRecord, error) {
	var (
		spotifyExpiry, mcpExpiry, createdAt       string
		redirectURIs, grantTypes, responseTypes   string
		r                                        TokenRecord
	)
	err := s.db.QueryRow(`SELECT
		spotify_access_token, spotify_refresh_token, spotify_token_expiry,
		mcp_access_token, mcp_refresh_token, mcp_token_expiry, created_at,
		redirect_uris, grant_types, response_types, token_endpoint_auth_method, client_name
		FROM tokens WHERE client_id = ?`, clientID).Scan(
		&r.SpotifyAccessToken, &r.SpotifyRefreshToken, &spotifyExpiry,
		&r.MCPAccessToken, &r.MCPRefreshToken, &mcpExpiry, &createdAt,
		&redirectURIs, &grantTypes, &responseTypes,
		&r.TokenEndpointAuthMethod, &r.ClientName,
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
	_ = json.Unmarshal([]byte(redirectURIs), &r.RedirectURIs)
	_ = json.Unmarshal([]byte(grantTypes), &r.GrantTypes)
	_ = json.Unmarshal([]byte(responseTypes), &r.ResponseTypes)
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
