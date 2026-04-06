package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2EOAuthFlow(t *testing.T) {
	// --- Setup: mock Spotify OAuth + MCP server ---
	mock := newMockSpotify(t)
	tokenStore := store.NewInMemoryTokenStore()

	// Short TTL so we can test token expiry without sleeping
	h := NewHandler(HandlerConfig{
		SpotifyClientID:      "test-spotify-client-id",
		SpotifyClientSecret:  "test-spotify-client-secret",
		SpotifyScopes:        []string{"user-read-playback-state"},
		Store:                tokenStore,
		SpotifyTokenEndpoint: mock.Server.URL,
		MCPTokenTTL:          200 * time.Millisecond,
	})

	// A recording handler behind auth middleware to verify access
	recorder := &recordingHandler{}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.Handle("POST /mcp", h.AuthMiddleware(recorder))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	h.SetBaseURL(ts.URL)

	noFollow := noRedirectClient()

	// --- Step 1: POST /register → receive client_id ---
	regReq, _ := json.Marshal(map[string]any{
		"redirect_uris": []string{"http://test-client/callback"},
	})
	resp, err := http.Post(ts.URL+"/register", "application/json", bytes.NewReader(regReq))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var regBody map[string]any
	json.NewDecoder(resp.Body).Decode(&regBody)
	clientID, ok := regBody["client_id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, clientID)

	// --- Step 2: GET /authorize → redirects to Spotify ---
	codeVerifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	challenge := CodeChallenge(codeVerifier)

	clientState := "e2e-csrf-state-12345"
	authResp, err := noFollow.Get(ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://test-client/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"state":                 {clientState},
	}.Encode())
	require.NoError(t, err)
	defer authResp.Body.Close()
	require.Equal(t, http.StatusFound, authResp.StatusCode)

	spotifyRedirect, err := url.Parse(authResp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "accounts.spotify.com", spotifyRedirect.Host)
	state := spotifyRedirect.Query().Get("state")
	require.NotEmpty(t, state)

	// --- Step 3: GET /callback (simulates Spotify redirecting back) ---
	cbResp, err := noFollow.Get(ts.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {state},
	}.Encode())
	require.NoError(t, err)
	defer cbResp.Body.Close()
	require.Equal(t, http.StatusFound, cbResp.StatusCode)

	clientRedirect, err := url.Parse(cbResp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "test-client", clientRedirect.Host)
	mcpCode := clientRedirect.Query().Get("code")
	require.NotEmpty(t, mcpCode, "should redirect with MCP auth code")
	assert.Equal(t, clientState, clientRedirect.Query().Get("state"),
		"callback redirect must round-trip the client's original state parameter")

	// --- Step 4: POST /token (authorization_code) → MCP tokens ---
	tokenResp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {mcpCode},
		"code_verifier": {codeVerifier},
	})
	require.NoError(t, err)
	defer tokenResp.Body.Close()
	require.Equal(t, http.StatusOK, tokenResp.StatusCode)

	var tokens map[string]any
	json.NewDecoder(tokenResp.Body).Decode(&tokens)
	accessToken, ok := tokens["access_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, accessToken)
	refreshToken, ok := tokens["refresh_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, refreshToken)

	// --- Step 5: POST /mcp with Bearer token → 200 ---
	req, err := http.NewRequest("POST", ts.URL+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	mcpResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	mcpResp.Body.Close()
	assert.Equal(t, http.StatusOK, mcpResp.StatusCode)

	// --- Step 6: Handler received correct client_id ---
	assert.True(t, recorder.called)
	assert.Equal(t, clientID, recorder.receivedClientID)

	// --- Step 7: Wait for token expiry, then refresh ---
	time.Sleep(250 * time.Millisecond) // TTL is 200ms

	// Old token should now be expired
	req2, err := http.NewRequest("POST", ts.URL+"/mcp", nil)
	require.NoError(t, err)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	expiredResp, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	expiredResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, expiredResp.StatusCode, "old token should be expired")

	// Refresh to get new tokens
	refreshResp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	require.NoError(t, err)
	defer refreshResp.Body.Close()
	require.Equal(t, http.StatusOK, refreshResp.StatusCode)

	var newTokens map[string]any
	json.NewDecoder(refreshResp.Body).Decode(&newTokens)
	newAccessToken := newTokens["access_token"].(string)
	require.NotEmpty(t, newAccessToken)
	assert.NotEqual(t, accessToken, newAccessToken, "new token should differ from old")

	// --- Step 8: POST /mcp with new token → 200 ---
	req3, err := http.NewRequest("POST", ts.URL+"/mcp", nil)
	require.NoError(t, err)
	req3.Header.Set("Authorization", "Bearer "+newAccessToken)
	newResp, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	newResp.Body.Close()
	assert.Equal(t, http.StatusOK, newResp.StatusCode)

	// --- Step 9: POST /mcp with old (rotated) token → 401 ---
	req4, err := http.NewRequest("POST", ts.URL+"/mcp", nil)
	require.NoError(t, err)
	req4.Header.Set("Authorization", "Bearer "+accessToken)
	oldResp, err := http.DefaultClient.Do(req4)
	require.NoError(t, err)
	oldResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, oldResp.StatusCode, "old rotated token should be rejected")
}

func TestE2ETokensSurviveRestart(t *testing.T) {
	// --- Setup: mock Spotify + first Handler instance ---
	mock := newMockSpotify(t)
	tokenStore := store.NewInMemoryTokenStore()

	h1 := NewHandler(HandlerConfig{
		SpotifyClientID:      "test-spotify-client-id",
		SpotifyClientSecret:  "test-spotify-client-secret",
		SpotifyScopes:        []string{"user-read-playback-state"},
		Store:                tokenStore,
		SpotifyTokenEndpoint: mock.Server.URL,
		MCPTokenTTL:          time.Hour,
	})

	recorder1 := &recordingHandler{}
	mux1 := http.NewServeMux()
	h1.RegisterRoutes(mux1)
	mux1.Handle("POST /mcp", h1.AuthMiddleware(recorder1))
	ts1 := httptest.NewServer(mux1)
	h1.SetBaseURL(ts1.URL)

	noFollow := noRedirectClient()

	// --- Register and complete full OAuth flow on first instance ---
	regReq, _ := json.Marshal(map[string]any{
		"redirect_uris": []string{"http://test-client/callback"},
		"client_name":   "TestClient",
	})
	resp, err := http.Post(ts1.URL+"/register", "application/json", bytes.NewReader(regReq))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var regBody map[string]any
	json.NewDecoder(resp.Body).Decode(&regBody)
	clientID := regBody["client_id"].(string)

	codeVerifier, err := GenerateCodeVerifier()
	require.NoError(t, err)
	challenge := CodeChallenge(codeVerifier)

	authResp, err := noFollow.Get(ts1.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://test-client/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
	}.Encode())
	require.NoError(t, err)
	defer authResp.Body.Close()

	spotifyRedirect, _ := url.Parse(authResp.Header.Get("Location"))
	state := spotifyRedirect.Query().Get("state")

	cbResp, err := noFollow.Get(ts1.URL + "/callback?" + url.Values{
		"code":  {"spotify-auth-code"},
		"state": {state},
	}.Encode())
	require.NoError(t, err)
	defer cbResp.Body.Close()

	clientRedirect, _ := url.Parse(cbResp.Header.Get("Location"))
	mcpCode := clientRedirect.Query().Get("code")

	tokenResp, err := http.PostForm(ts1.URL+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {mcpCode},
		"code_verifier": {codeVerifier},
	})
	require.NoError(t, err)
	defer tokenResp.Body.Close()

	var tokens map[string]any
	json.NewDecoder(tokenResp.Body).Decode(&tokens)
	accessToken := tokens["access_token"].(string)
	refreshToken := tokens["refresh_token"].(string)

	// Verify token works on first instance
	req, _ := http.NewRequest("POST", ts1.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	mcpResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	mcpResp.Body.Close()
	require.Equal(t, http.StatusOK, mcpResp.StatusCode)

	// --- "Restart": shut down first server, create NEW Handler with SAME store ---
	ts1.Close()

	h2 := NewHandler(HandlerConfig{
		SpotifyClientID:      "test-spotify-client-id",
		SpotifyClientSecret:  "test-spotify-client-secret",
		SpotifyScopes:        []string{"user-read-playback-state"},
		Store:                tokenStore,
		SpotifyTokenEndpoint: mock.Server.URL,
		MCPTokenTTL:          time.Hour,
	})

	recorder2 := &recordingHandler{}
	mux2 := http.NewServeMux()
	h2.RegisterRoutes(mux2)
	mux2.Handle("POST /mcp", h2.AuthMiddleware(recorder2))
	ts2 := httptest.NewServer(mux2)
	defer ts2.Close()
	h2.SetBaseURL(ts2.URL)

	// --- Old access token should work on new instance ---
	req2, _ := http.NewRequest("POST", ts2.URL+"/mcp", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	mcpResp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	mcpResp2.Body.Close()
	assert.Equal(t, http.StatusOK, mcpResp2.StatusCode, "access token should survive restart")
	assert.Equal(t, clientID, recorder2.receivedClientID)

	// --- Old refresh token should produce new tokens on new instance ---
	refreshResp, err := http.PostForm(ts2.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	require.NoError(t, err)
	defer refreshResp.Body.Close()
	assert.Equal(t, http.StatusOK, refreshResp.StatusCode, "refresh token should survive restart")

	var newTokens map[string]any
	json.NewDecoder(refreshResp.Body).Decode(&newTokens)
	newAccessToken := newTokens["access_token"].(string)
	require.NotEmpty(t, newAccessToken)

	// New access token should also work
	req3, _ := http.NewRequest("POST", ts2.URL+"/mcp", nil)
	req3.Header.Set("Authorization", "Bearer "+newAccessToken)
	mcpResp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	mcpResp3.Body.Close()
	assert.Equal(t, http.StatusOK, mcpResp3.StatusCode)
}
