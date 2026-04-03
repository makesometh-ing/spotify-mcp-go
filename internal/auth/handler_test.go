package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandler(t *testing.T) (*httptest.Server, *Handler) {
	t.Helper()
	h := NewHandler("") // base URL filled in after server starts
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
