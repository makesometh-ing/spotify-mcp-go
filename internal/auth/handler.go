package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

// PendingAuth holds the state for an in-progress authorization flow.
type PendingAuth struct {
	ClientID        string
	RedirectURI     string
	CodeChallenge   string
	SpotifyVerifier string
}

// PendingCode holds the state for a pending MCP authorization code exchange.
type PendingCode struct {
	ClientID      string
	CodeChallenge string
}

// HandlerConfig holds the configuration for creating a Handler.
type HandlerConfig struct {
	SpotifyClientID      string
	SpotifyClientSecret  string
	SpotifyScopes        []string
	Store                store.TokenStore
	SpotifyTokenEndpoint string // optional override for testing
}

// Handler implements the OAuth proxy HTTP endpoints for MCP clients.
type Handler struct {
	mu              sync.RWMutex
	baseURL         string
	spotifyClientID string
	spotifyScopes   []string
	store           store.TokenStore
	pendingAuths    map[string]PendingAuth // keyed by state parameter
	spotifyClient   *SpotifyClient
	pendingCodes    map[string]PendingCode // keyed by MCP auth code
}

// NewHandler creates a Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	endpoint := cfg.SpotifyTokenEndpoint
	if endpoint == "" {
		endpoint = defaultSpotifyTokenEndpoint
	}
	return &Handler{
		spotifyClientID: cfg.SpotifyClientID,
		spotifyScopes:   cfg.SpotifyScopes,
		store:           cfg.Store,
		pendingAuths:    make(map[string]PendingAuth),
		spotifyClient: &SpotifyClient{
			ClientID:      cfg.SpotifyClientID,
			ClientSecret:  cfg.SpotifyClientSecret,
			TokenEndpoint: endpoint,
		},
		pendingCodes: make(map[string]PendingCode),
	}
}

// SetBaseURL updates the base URL used in metadata responses.
func (h *Handler) SetBaseURL(u string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.baseURL = u
}

func (h *Handler) getBaseURL() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.baseURL
}

// GetPendingAuth retrieves pending authorization state by the state parameter.
func (h *Handler) GetPendingAuth(state string) (PendingAuth, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.pendingAuths[state]
	return p, ok
}

// GetPendingCode retrieves a pending MCP auth code exchange.
func (h *Handler) GetPendingCode(code string) (PendingCode, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.pendingCodes[code]
	return p, ok
}

// RegisterRoutes registers all OAuth proxy routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.handleAuthorizationServer)
	mux.HandleFunc("POST /register", h.handleRegister)
	mux.HandleFunc("GET /authorize", h.handleAuthorize)
	mux.HandleFunc("GET /callback", h.handleCallback)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	state := r.URL.Query().Get("state")

	h.mu.Lock()
	pending, ok := h.pendingAuths[state]
	if !ok {
		h.mu.Unlock()
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}
	delete(h.pendingAuths, state)
	h.mu.Unlock()

	// Exchange the Spotify authorization code for tokens
	callbackURI := h.getBaseURL() + "/callback"
	tokenResp, err := h.spotifyClient.ExchangeCode(r.Context(), code, callbackURI, pending.SpotifyVerifier)
	if err != nil {
		http.Error(w, "spotify token exchange failed", http.StatusBadGateway)
		return
	}

	// Update the token record with Spotify tokens
	record, err := h.store.Load(r.Context(), pending.ClientID)
	if err != nil || record == nil {
		record = &store.TokenRecord{}
	}
	record.SpotifyAccessToken = tokenResp.AccessToken
	record.SpotifyRefreshToken = tokenResp.RefreshToken
	record.SpotifyTokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	if err := h.store.Store(r.Context(), pending.ClientID, record); err != nil {
		http.Error(w, "failed to store tokens", http.StatusInternalServerError)
		return
	}

	// Generate an MCP auth code for the client to exchange at /token
	mcpCode, err := GenerateAuthCode()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.pendingCodes[mcpCode] = PendingCode{
		ClientID:      pending.ClientID,
		CodeChallenge: pending.CodeChallenge,
	}
	h.mu.Unlock()

	// Redirect to the MCP client's redirect_uri with the MCP auth code
	redirectURL, err := url.Parse(pending.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := redirectURL.Query()
	q.Set("code", mcpCode)
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (h *Handler) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	base := h.getBaseURL()
	resp := map[string]any{
		"resource":              base,
		"authorization_servers": []string{base},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	base := h.getBaseURL()
	resp := map[string]any{
		"issuer":                           base,
		"authorization_endpoint":           base + "/authorize",
		"token_endpoint":                   base + "/token",
		"registration_endpoint":            base + "/register",
		"response_types_supported":         []string{"code"},
		"grant_types_supported":            []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported": []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	clientID, err := GenerateToken()
	if err != nil {
		http.Error(w, "failed to generate client_id", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	record := &store.TokenRecord{
		CreatedAt: now,
	}
	if err := h.store.Store(r.Context(), clientID, record); err != nil {
		http.Error(w, "failed to store client registration", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"client_id":           clientID,
		"client_id_issued_at": now.Unix(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		http.Error(w, "missing client_id", http.StatusBadRequest)
		return
	}

	record, err := h.store.Load(r.Context(), clientID)
	if err != nil || record == nil {
		http.Error(w, "unregistered client_id", http.StatusBadRequest)
		return
	}

	redirectURI := r.URL.Query().Get("redirect_uri")
	codeChallenge := r.URL.Query().Get("code_challenge")

	// Generate server-side PKCE for Spotify
	spotifyVerifier, err := GenerateCodeVerifier()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	spotifyChallenge := CodeChallenge(spotifyVerifier)

	// Generate state parameter to link callback back to this auth
	state, err := GenerateToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Store pending auth state
	h.mu.Lock()
	h.pendingAuths[state] = PendingAuth{
		ClientID:        clientID,
		RedirectURI:     redirectURI,
		CodeChallenge:   codeChallenge,
		SpotifyVerifier: spotifyVerifier,
	}
	h.mu.Unlock()

	// Build Spotify authorize URL
	spotifyURL := url.URL{
		Scheme: "https",
		Host:   "accounts.spotify.com",
		Path:   "/authorize",
	}
	q := spotifyURL.Query()
	q.Set("client_id", h.spotifyClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", h.getBaseURL()+"/callback")
	q.Set("code_challenge", spotifyChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("scope", strings.Join(h.spotifyScopes, " "))
	spotifyURL.RawQuery = q.Encode()

	http.Redirect(w, r, spotifyURL.String(), http.StatusFound)
}
