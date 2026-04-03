package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
