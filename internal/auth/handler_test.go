package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandler(t *testing.T) (*httptest.Server, *Handler) {
	t.Helper()
	tokenStore := store.NewInMemoryTokenStore()
	h := NewHandler("", tokenStore)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	h.SetBaseURL(ts.URL)
	t.Cleanup(ts.Close)
	return ts, h
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
