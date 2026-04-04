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

	authResp, err := noFollow.Get(ts.URL + "/authorize?" + url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"http://test-client/callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
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
