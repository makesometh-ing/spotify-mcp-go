package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandler(t *testing.T) (*httptest.Server, *Handler) {
	t.Helper()
	tokenStore := store.NewInMemoryTokenStore()
	h := NewHandler(HandlerConfig{
		SpotifyClientID: "test-spotify-client-id",
		SpotifyScopes:   []string{"user-read-playback-state", "user-modify-playback-state"},
		Store:           tokenStore,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	h.SetBaseURL(ts.URL)
	t.Cleanup(ts.Close)
	return ts, h
}

// registerClient registers a new client and returns the client_id.
func registerClient(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(ts.URL+"/register", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return body["client_id"].(string)
}

func TestWellKnownProtectedResource(t *testing.T) {
	ts, _ := setupTestHandler(t)

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]any
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	assert.Equal(t, ts.URL, body["resource"])

	authServers, ok := body["authorization_servers"].([]any)
	require.True(t, ok, "authorization_servers should be an array")
	require.Len(t, authServers, 1)
	assert.Equal(t, ts.URL, authServers[0])
}

func TestWellKnownAuthorizationServer(t *testing.T) {
	ts, _ := setupTestHandler(t)

	resp, err := http.Get(ts.URL + "/.well-known/oauth-authorization-server")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]any
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	assert.Equal(t, ts.URL+"/authorize", body["authorization_endpoint"])
	assert.Equal(t, ts.URL+"/token", body["token_endpoint"])
	assert.Equal(t, ts.URL+"/register", body["registration_endpoint"])

	responseTypes, ok := body["response_types_supported"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"code"}, responseTypes)

	grantTypes, ok := body["grant_types_supported"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"authorization_code", "refresh_token"}, grantTypes)

	challengeMethods, ok := body["code_challenge_methods_supported"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"S256"}, challengeMethods)
}

func TestRegisterReturns201WithClientID(t *testing.T) {
	ts, _ := setupTestHandler(t)

	resp, err := http.Post(ts.URL+"/register", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]any
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	clientID, ok := body["client_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, clientID)

	issuedAt, ok := body["client_id_issued_at"].(float64)
	require.True(t, ok)
	assert.Greater(t, issuedAt, float64(0))
}

func TestRegisterReturnsDifferentClientIDs(t *testing.T) {
	ts, _ := setupTestHandler(t)

	resp1, err := http.Post(ts.URL+"/register", "application/json", nil)
	require.NoError(t, err)
	defer resp1.Body.Close()
	var body1 map[string]any
	json.NewDecoder(resp1.Body).Decode(&body1)

	resp2, err := http.Post(ts.URL+"/register", "application/json", nil)
	require.NoError(t, err)
	defer resp2.Body.Close()
	var body2 map[string]any
	json.NewDecoder(resp2.Body).Decode(&body2)

	assert.NotEqual(t, body1["client_id"], body2["client_id"])
}

func TestRegisterGETReturns405(t *testing.T) {
	ts, _ := setupTestHandler(t)

	resp, err := http.Get(ts.URL + "/register")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestRegisterStoresClientInTokenStore(t *testing.T) {
	ts, h := setupTestHandler(t)

	resp, err := http.Post(ts.URL+"/register", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	clientID := body["client_id"].(string)

	record, err := h.store.Load(t.Context(), clientID)
	require.NoError(t, err)
	require.NotNil(t, record, "registered client_id should be loadable from the token store")
}

func TestAuthorizeRedirectsToSpotify(t *testing.T) {
	ts, _ := setupTestHandler(t)
	clientID := registerClient(t, ts)

	// Don't follow redirects
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	u := ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://localhost:9999/callback"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode()

	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)

	assert.Equal(t, "accounts.spotify.com", loc.Host)
	assert.Equal(t, "/authorize", loc.Path)
	assert.Equal(t, "test-spotify-client-id", loc.Query().Get("client_id"))
	assert.Equal(t, ts.URL+"/callback", loc.Query().Get("redirect_uri"))
	assert.Equal(t, "S256", loc.Query().Get("code_challenge_method"))
	assert.NotEmpty(t, loc.Query().Get("code_challenge"))
	assert.NotEmpty(t, loc.Query().Get("state"))
	assert.Equal(t, "code", loc.Query().Get("response_type"))
	assert.Contains(t, loc.Query().Get("scope"), "user-read-playback-state")
}

func TestAuthorizeStoresPendingState(t *testing.T) {
	ts, h := setupTestHandler(t)
	clientID := registerClient(t, ts)

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	u := ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://localhost:9999/callback"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode()

	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	state := loc.Query().Get("state")

	pending, ok := h.GetPendingAuth(state)
	require.True(t, ok)
	assert.Equal(t, clientID, pending.ClientID)
	assert.Equal(t, "http://localhost:9999/callback", pending.RedirectURI)
	assert.Equal(t, "test-challenge", pending.CodeChallenge)
	assert.NotEmpty(t, pending.SpotifyVerifier)
}

func TestAuthorizeMissingClientID(t *testing.T) {
	ts, _ := setupTestHandler(t)

	u := ts.URL + "/authorize?" + url.Values{
		"redirect_uri":          {"http://localhost:9999/callback"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode()

	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAuthorizeUnregisteredClientID(t *testing.T) {
	ts, _ := setupTestHandler(t)

	u := ts.URL + "/authorize?" + url.Values{
		"client_id":             {"unknown-client"},
		"redirect_uri":          {"http://localhost:9999/callback"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode()

	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- Callback endpoint tests ---

type mockSpotify struct {
	Server       *httptest.Server
	LastForm     url.Values
	ResponseCode int
	Response     map[string]any
	mu           sync.Mutex
}

func newMockSpotify(t *testing.T) *mockSpotify {
	t.Helper()
	m := &mockSpotify{
		ResponseCode: http.StatusOK,
		Response: map[string]any{
			"access_token":  "spotify-access-token",
			"refresh_token": "spotify-refresh-token",
			"expires_in":    float64(3600),
			"token_type":    "Bearer",
		},
	}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		m.mu.Lock()
		m.LastForm = r.PostForm
		code := m.ResponseCode
		resp := m.Response
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(m.Server.Close)
	return m
}

// setupCallbackTest creates a handler with a mock Spotify token endpoint,
// registers a client, initiates an auth flow, and returns the state parameter
// needed to call GET /callback.
func setupCallbackTest(t *testing.T) (ts *httptest.Server, h *Handler, mock *mockSpotify, state string, clientID string) {
	t.Helper()
	mock = newMockSpotify(t)

	tokenStore := store.NewInMemoryTokenStore()
	h = NewHandler(HandlerConfig{
		SpotifyClientID:      "test-spotify-client-id",
		SpotifyClientSecret:  "test-spotify-client-secret",
		SpotifyScopes:        []string{"user-read-playback-state"},
		Store:                tokenStore,
		SpotifyTokenEndpoint: mock.Server.URL,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts = httptest.NewServer(mux)
	h.SetBaseURL(ts.URL)
	t.Cleanup(ts.Close)

	// Register a client
	clientID = registerClient(t, ts)

	// Start auth flow to create pending state
	client := noRedirectClient()
	u := ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://localhost:9999/callback"},
		"code_challenge":        {"test-challenge"},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode()
	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	state = loc.Query().Get("state")

	return ts, h, mock, state, clientID
}

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func TestCallbackExchangesCodeWithSpotify(t *testing.T) {
	ts, h, mock, state, _ := setupCallbackTest(t)

	// Capture the stored PKCE verifier before callback consumes the pending auth
	pending, ok := h.GetPendingAuth(state)
	require.True(t, ok)
	expectedVerifier := pending.SpotifyVerifier

	client := noRedirectClient()
	u := ts.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {state},
	}.Encode()

	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	mock.mu.Lock()
	form := mock.LastForm
	mock.mu.Unlock()

	assert.Equal(t, "authorization_code", form.Get("grant_type"))
	assert.Equal(t, "spotify-auth-code", form.Get("code"))
	assert.Equal(t, ts.URL+"/callback", form.Get("redirect_uri"))
	assert.Equal(t, "test-spotify-client-id", form.Get("client_id"))
	assert.Equal(t, "test-spotify-client-secret", form.Get("client_secret"))
	assert.Equal(t, expectedVerifier, form.Get("code_verifier"))
}

func TestCallbackStoresSpotifyTokens(t *testing.T) {
	ts, h, _, state, clientID := setupCallbackTest(t)
	client := noRedirectClient()

	u := ts.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {state},
	}.Encode()

	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	record, err := h.store.Load(t.Context(), clientID)
	require.NoError(t, err)
	assert.Equal(t, "spotify-access-token", record.SpotifyAccessToken)
	assert.Equal(t, "spotify-refresh-token", record.SpotifyRefreshToken)
	assert.False(t, record.SpotifyTokenExpiry.IsZero())
}

func TestCallbackRedirectsWithMCPCode(t *testing.T) {
	ts, _, _, state, _ := setupCallbackTest(t)
	client := noRedirectClient()

	u := ts.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {state},
	}.Encode()

	resp, err := client.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "localhost:9999", loc.Host)
	assert.Equal(t, "/callback", loc.Path)
	assert.NotEmpty(t, loc.Query().Get("code"), "should include MCP auth code")
}

func TestCallbackInvalidStateReturns400(t *testing.T) {
	ts, _, _, _, _ := setupCallbackTest(t)

	u := ts.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {"invalid-state"},
	}.Encode()

	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCallbackMissingCodeReturns400(t *testing.T) {
	ts, _, _, state, _ := setupCallbackTest(t)

	u := ts.URL + "/callback?" + url.Values{
		"state": {state},
	}.Encode()

	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCallbackSpotifyFailureReturns502(t *testing.T) {
	ts, _, mock, state, _ := setupCallbackTest(t)

	mock.mu.Lock()
	mock.ResponseCode = http.StatusBadRequest
	mock.Response = map[string]any{"error": "invalid_grant"}
	mock.mu.Unlock()

	u := ts.URL + "/callback?" + url.Values{
		"code":  {"bad-code"},
		"state": {state},
	}.Encode()

	resp, err := http.Get(u)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}
