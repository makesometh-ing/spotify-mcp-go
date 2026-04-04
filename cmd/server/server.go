package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth"
	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/makesometh-ing/spotify-mcp-go/internal/tools"
)

type serverConfig struct {
	Port                 string
	SpotifyClientID      string
	SpotifyClientSecret  string
	TokenDBPath          string
	BaseURL              string // SPOTIFY_MCP_BASE_URL; empty = http://127.0.0.1:<port>
	SpotifyTokenEndpoint string // override for testing; empty = Spotify default
	SpotifyAPIBaseURL    string // override for testing; empty = defaultSpotifyAPIBaseURL
}

const defaultSpotifyAPIBaseURL = "https://api.spotify.com/v1"

// loadConfig reads configuration from environment variables and an optional .env file.
// Environment variables take precedence over .env file values.
func loadConfig(envFilePath string) (*serverConfig, error) {
	envMap := make(map[string]string)
	if envFilePath != "" {
		m, err := readEnvFile(envFilePath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading .env: %w", err)
		}
		if m != nil {
			envMap = m
		}
	}

	get := func(key, defaultValue string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v, ok := envMap[key]; ok && v != "" {
			return v
		}
		return defaultValue
	}

	cfg := &serverConfig{
		Port:                get("SPOTIFY_MCP_PORT", "8080"),
		SpotifyClientID:     get("SPOTIFY_CLIENT_ID", ""),
		SpotifyClientSecret: get("SPOTIFY_CLIENT_SECRET", ""),
		TokenDBPath:         get("SPOTIFY_MCP_TOKEN_DB", "~/.config/spotify-mcp-go/auth/tokens.db"),
		BaseURL:             get("SPOTIFY_MCP_BASE_URL", ""),
	}

	if cfg.SpotifyClientID == "" {
		return nil, fmt.Errorf("SPOTIFY_CLIENT_ID is required but not set")
	}
	if cfg.SpotifyClientSecret == "" {
		return nil, fmt.Errorf("SPOTIFY_CLIENT_SECRET is required but not set")
	}

	return cfg, nil
}

// newLogger creates a SugaredLogger based on the debug flag.
// When debug is false, returns a no-op logger (zero output).
// When debug is true, returns a development logger with console encoding to stderr.
func newLogger(debug bool) *zap.SugaredLogger {
	if !debug {
		return zap.NewNop().Sugar()
	}
	cfg := zap.NewDevelopmentConfig()
	cfg.EncoderConfig.EncodeLevel = zap.NewDevelopmentConfig().EncoderConfig.EncodeLevel
	logger, err := cfg.Build()
	if err != nil {
		// Fall back to nop if config fails (should not happen)
		return zap.NewNop().Sugar()
	}
	return logger.Sugar()
}

func printStartupInfo(out io.Writer, port string, baseURL string) {
	base := baseURL
	if base == "" {
		base = "http://127.0.0.1:" + port
	}
	_, _ = fmt.Fprintf(out, "MCP endpoint: %s/mcp\n", base)
	_, _ = fmt.Fprintf(out, "Callback URL: %s/callback\n", base)
	_, _ = fmt.Fprintf(out, "SPOTIFY_CLIENT_ID: set\n")
	_, _ = fmt.Fprintf(out, "SPOTIFY_CLIENT_SECRET: set\n")
	_, _ = fmt.Fprintf(out, "\nConfigure the callback URL above as a Redirect URI in your Spotify Developer Dashboard at https://developer.spotify.com/dashboard\n")
}

// run starts the MCP server. It blocks until ctx is cancelled. When the server
// is ready to accept connections, the listen address is sent on addrCh (if non-nil).
// toolRegs may be nil if no tools are registered.
func run(ctx context.Context, cfg *serverConfig, toolRegs []tools.ToolRegistration, spotifyScopes []string, out io.Writer, addrCh chan<- string, logger *zap.SugaredLogger) error {
	dbPath := cfg.TokenDBPath
	if strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		dbPath = home + dbPath[1:]
	}
	sqliteStore, err := store.NewSQLiteTokenStore(dbPath)
	if err != nil {
		return fmt.Errorf("opening token store: %w", err)
	}
	tokenStore := store.NewLoggingTokenStore(sqliteStore, logger)

	spotifyClient := &auth.SpotifyClient{
		ClientID:      cfg.SpotifyClientID,
		ClientSecret:  cfg.SpotifyClientSecret,
		TokenEndpoint: cfg.SpotifyTokenEndpoint,
	}

	authHandler := auth.NewHandler(auth.HandlerConfig{
		SpotifyClientID:      cfg.SpotifyClientID,
		SpotifyClientSecret:  cfg.SpotifyClientSecret,
		SpotifyScopes:        spotifyScopes,
		Store:                tokenStore,
		SpotifyTokenEndpoint: cfg.SpotifyTokenEndpoint,
		Logger:               logger,
	})

	mcpServer := server.NewMCPServer("spotify-mcp-go", "1.0.0",
		server.WithToolCapabilities(false),
	)

	apiBase := cfg.SpotifyAPIBaseURL
	if apiBase == "" {
		apiBase = defaultSpotifyAPIBaseURL
	}
	if toolRegs != nil {
		tools.Register(mcpServer, toolRegs, tokenStore, spotifyClient, apiBase, logger)
	}

	httpTransport := server.NewStreamableHTTPServer(mcpServer)

	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)
	mux.Handle("/mcp", authHandler.AuthMiddleware(httpTransport))

	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("listening on port %s: %w", cfg.Port, err)
	}

	addr := listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:" + port
	}
	authHandler.SetBaseURL(baseURL)
	printStartupInfo(out, port, cfg.BaseURL)
	logger.Infow("server started", "address", addr, "base_url", baseURL)

	if addrCh != nil {
		addrCh <- addr
	}

	srv := &http.Server{Handler: httpLoggingMiddleware(logger, mux)}
	go func() {
		<-ctx.Done()
		logger.Info("server shutting down")
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// httpLoggingMiddleware logs method, path, status code, and duration for every request.
func httpLoggingMiddleware(logger *zap.SugaredLogger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Infow("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// readEnvFile parses a .env file into a key-value map.
// Supports KEY=VALUE lines, # comments, and blank lines.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result, scanner.Err()
}
