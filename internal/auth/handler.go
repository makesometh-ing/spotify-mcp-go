package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
)

type contextKey string

const clientIDContextKey contextKey = "client_id"

// ClientIDFromContext extracts the authenticated client ID from the request context.
func ClientIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(clientIDContextKey).(string)
	return v, ok
}

// ContextWithClientID returns a context carrying the given client ID.
func ContextWithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDContextKey, clientID)
}

// AuthMiddleware returns a handler that validates the Bearer token in the
// Authorization header and sets the client ID in the request context.
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			h.writeUnauthorized(w)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		clientID, ok := h.tokenManager.ValidateAccessToken(token)
		if !ok {
			h.writeUnauthorized(w)
			return
		}

		ctx := context.WithValue(r.Context(), clientIDContextKey, clientID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) writeUnauthorized(w http.ResponseWriter) {
	base := h.getBaseURL()
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource"`)
	w.WriteHeader(http.StatusUnauthorized)
}

// PendingAuth holds the state for an in-progress authorization flow.
type PendingAuth struct {
	ClientID        string
	RedirectURI     string
	CodeChallenge   string
	SpotifyVerifier string
	ClientState     string
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
	SpotifyTokenEndpoint string            // optional override for testing
	MCPTokenTTL          time.Duration     // optional, defaults to 1 hour
	Logger               *zap.SugaredLogger // optional, defaults to nop
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
	tokenManager    *TokenManager
	logger          *zap.SugaredLogger
}

// NewHandler creates a Handler with the given configuration.
func NewHandler(cfg HandlerConfig) *Handler {
	endpoint := cfg.SpotifyTokenEndpoint
	if endpoint == "" {
		endpoint = defaultSpotifyTokenEndpoint
	}
	ttl := cfg.MCPTokenTTL
	if ttl == 0 {
		ttl = time.Hour
	}
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	tm := NewTokenManager(ttl)

	// Hydrate TokenManager from persisted records so MCP tokens
	// survive server restarts.
	if records, err := cfg.Store.LoadAll(context.Background()); err == nil {
		tm.Hydrate(records)
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
		tokenManager: tm,
		logger:       logger.Named("auth"),
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
	mux.HandleFunc("POST /token", h.handleToken)
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

	h.logger.Infow("callback received", "client_id", pending.ClientID)

	// Exchange the Spotify authorization code for tokens
	callbackURI := h.getBaseURL() + "/callback"
	tokenResp, err := h.spotifyClient.ExchangeCode(r.Context(), code, callbackURI, pending.SpotifyVerifier)
	if err != nil {
		h.logger.Errorw("spotify token exchange failed", "client_id", pending.ClientID, "error", err)
		http.Error(w, "spotify token exchange failed", http.StatusBadGateway)
		return
	}
	h.logger.Infow("spotify token exchange", "client_id", pending.ClientID)

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
	if pending.ClientState != "" {
		q.Set("state", pending.ClientState)
	}
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, "invalid_request", http.StatusBadRequest)
		return
	}

	grantType := r.FormValue("grant_type")
	h.logger.Debugw("token request", "grant_type", grantType)

	switch grantType {
	case "authorization_code":
		h.handleTokenCodeExchange(w, r)
	case "refresh_token":
		h.handleTokenRefresh(w, r)
	default:
		writeTokenError(w, "unsupported_grant_type", http.StatusBadRequest)
	}
}

func (h *Handler) handleTokenCodeExchange(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	if code == "" {
		writeTokenError(w, "invalid_request", http.StatusBadRequest)
		return
	}

	codeVerifier := r.FormValue("code_verifier")

	// Look up and consume the pending code (one-time use)
	h.mu.Lock()
	pending, ok := h.pendingCodes[code]
	if ok {
		delete(h.pendingCodes, code)
	}
	h.mu.Unlock()

	if !ok {
		writeTokenError(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	// Verify PKCE
	if !VerifyCodeChallenge(codeVerifier, pending.CodeChallenge) {
		writeTokenError(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	// Issue MCP tokens
	accessToken, err := h.tokenManager.IssueAccessToken(pending.ClientID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	refreshToken, err := h.tokenManager.IssueRefreshToken(pending.ClientID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update store record with MCP tokens
	record, err := h.store.Load(r.Context(), pending.ClientID)
	if err == nil && record != nil {
		record.MCPAccessToken = accessToken
		record.MCPRefreshToken = refreshToken
		record.MCPTokenExpiry = time.Now().Add(h.tokenManager.TTL())
		_ = h.store.Store(r.Context(), pending.ClientID, record)
	}

	h.logger.Infow("token exchange", "client_id", pending.ClientID)

	writeTokenResponse(w, accessToken, refreshToken, int(h.tokenManager.TTL().Seconds()))
}

func (h *Handler) handleTokenRefresh(w http.ResponseWriter, r *http.Request) {
	oldRefresh := r.FormValue("refresh_token")

	clientID, ok := h.tokenManager.ValidateRefreshToken(oldRefresh)
	if !ok {
		writeTokenError(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	// Invalidate old refresh token (rotation)
	h.tokenManager.InvalidateRefreshToken(oldRefresh)

	// Issue new tokens
	accessToken, err := h.tokenManager.IssueAccessToken(clientID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	refreshToken, err := h.tokenManager.IssueRefreshToken(clientID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update store record
	record, err := h.store.Load(r.Context(), clientID)
	if err == nil && record != nil {
		record.MCPAccessToken = accessToken
		record.MCPRefreshToken = refreshToken
		record.MCPTokenExpiry = time.Now().Add(h.tokenManager.TTL())
		_ = h.store.Store(r.Context(), clientID, record)
	}

	h.logger.Infow("token refresh", "client_id", clientID)

	writeTokenResponse(w, accessToken, refreshToken, int(h.tokenManager.TTL().Seconds()))
}

func writeTokenError(w http.ResponseWriter, errCode string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errCode})
}

func writeTokenResponse(w http.ResponseWriter, accessToken, refreshToken string, expiresIn int) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}

func (h *Handler) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	base := h.getBaseURL()
	resp := map[string]any{
		"resource":              base,
		"authorization_servers": []string{base},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	base := h.getBaseURL()
	resp := map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"registration_endpoint":                 base + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// registrationRequest holds the parsed RFC 7591 registration request body.
type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientName              string   `json:"client_name"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registrationRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Apply RFC 7591 Section 2 defaults
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none"
	}

	clientID, err := GenerateToken()
	if err != nil {
		http.Error(w, "failed to generate client_id", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	record := &store.TokenRecord{
		CreatedAt:               now,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		ClientName:              req.ClientName,
	}
	if err := h.store.Store(r.Context(), clientID, record); err != nil {
		http.Error(w, "failed to store client registration", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        now.Unix(),
		"grant_types":                req.GrantTypes,
		"response_types":             req.ResponseTypes,
		"token_endpoint_auth_method": req.TokenEndpointAuthMethod,
	}
	if len(req.RedirectURIs) > 0 {
		resp["redirect_uris"] = req.RedirectURIs
	}
	if req.ClientName != "" {
		resp["client_name"] = req.ClientName
	}

	h.logger.Infow("client registered", "client_id", clientID, "client_name", req.ClientName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
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

	// Validate redirect_uri against registered URIs (if any were registered)
	if len(record.RedirectURIs) > 0 && !slices.Contains(record.RedirectURIs, redirectURI) {
		http.Error(w, "redirect_uri does not match registered URIs", http.StatusBadRequest)
		return
	}

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

	h.logger.Infow("authorize request", "client_id", clientID, "redirect_uri", redirectURI)

	// Capture the client's state parameter (opaque, for round-tripping)
	clientState := r.URL.Query().Get("state")

	// Store pending auth state
	h.mu.Lock()
	h.pendingAuths[state] = PendingAuth{
		ClientID:        clientID,
		RedirectURI:     redirectURI,
		CodeChallenge:   codeChallenge,
		SpotifyVerifier: spotifyVerifier,
		ClientState:     clientState,
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
