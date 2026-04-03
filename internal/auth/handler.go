package auth

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Handler implements the OAuth proxy HTTP endpoints for MCP clients.
type Handler struct {
	mu      sync.RWMutex
	baseURL string
}

// NewHandler creates a Handler with the given base URL.
// If baseURL is empty, call SetBaseURL before serving requests.
func NewHandler(baseURL string) *Handler {
	return &Handler{baseURL: baseURL}
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

// RegisterRoutes registers all OAuth proxy routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.handleAuthorizationServer)
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
		"issuer":                             base,
		"authorization_endpoint":             base + "/authorize",
		"token_endpoint":                     base + "/token",
		"registration_endpoint":              base + "/register",
		"response_types_supported":           []string{"code"},
		"grant_types_supported":              []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":   []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
