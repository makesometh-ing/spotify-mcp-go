package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/makesometh-ing/spotify-mcp-go/internal/auth/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockSpotifyTokenServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func defaultMockHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-access-token",
			"refresh_token": "refreshed-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}
}

func TestSpotifyTokenRefreshValidTokenNoRefresh(t *testing.T) {
	var refreshCalled atomic.Int32
	mock := mockSpotifyTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalled.Add(1)
		defaultMockHandler()(w, r)
	})

	tokenStore := store.NewInMemoryTokenStore()
	err := tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "valid-token",
		SpotifyRefreshToken: "refresh-token",
		SpotifyTokenExpiry:  time.Now().Add(time.Hour), // not expired
	})
	require.NoError(t, err)

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenEndpoint: mock.URL,
	})

	token, err := refresher.GetAccessToken(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Equal(t, "valid-token", token)
	assert.Equal(t, int32(0), refreshCalled.Load(), "should not call Spotify when token is valid")
}

func TestSpotifyTokenRefreshExpiredTriggersRefresh(t *testing.T) {
	var refreshCalled atomic.Int32
	mock := mockSpotifyTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalled.Add(1)
		defaultMockHandler()(w, r)
	})

	tokenStore := store.NewInMemoryTokenStore()
	err := tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "expired-token",
		SpotifyRefreshToken: "refresh-token",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour), // expired
	})
	require.NoError(t, err)

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		TokenEndpoint: mock.URL,
	})

	token, err := refresher.GetAccessToken(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token)
	assert.Equal(t, int32(1), refreshCalled.Load())
}

func TestSpotifyTokenRefreshRequestParams(t *testing.T) {
	var lastForm map[string]string
	mock := mockSpotifyTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		lastForm = map[string]string{
			"grant_type":    r.PostForm.Get("grant_type"),
			"refresh_token": r.PostForm.Get("refresh_token"),
			"client_id":     r.PostForm.Get("client_id"),
			"client_secret": r.PostForm.Get("client_secret"),
		}
		defaultMockHandler()(w, r)
	})

	tokenStore := store.NewInMemoryTokenStore()
	tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "expired",
		SpotifyRefreshToken: "my-refresh-token",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour),
	})

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "my-client-id",
		ClientSecret:  "my-client-secret",
		TokenEndpoint: mock.URL,
	})

	_, err := refresher.GetAccessToken(context.Background(), "client-1")
	require.NoError(t, err)

	assert.Equal(t, "refresh_token", lastForm["grant_type"])
	assert.Equal(t, "my-refresh-token", lastForm["refresh_token"])
	assert.Equal(t, "my-client-id", lastForm["client_id"])
	assert.Equal(t, "my-client-secret", lastForm["client_secret"])
}

func TestSpotifyTokenRefreshStoresNewTokens(t *testing.T) {
	mock := mockSpotifyTokenServer(t, defaultMockHandler())

	tokenStore := store.NewInMemoryTokenStore()
	tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "old-token",
		SpotifyRefreshToken: "old-refresh",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour),
	})

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenEndpoint: mock.URL,
	})

	_, err := refresher.GetAccessToken(context.Background(), "client-1")
	require.NoError(t, err)

	record, err := tokenStore.Load(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", record.SpotifyAccessToken)
	assert.Equal(t, "refreshed-refresh-token", record.SpotifyRefreshToken)
	assert.True(t, record.SpotifyTokenExpiry.After(time.Now()))
}

func TestSpotifyTokenRefreshUsesNewTokenForCall(t *testing.T) {
	mock := mockSpotifyTokenServer(t, defaultMockHandler())

	tokenStore := store.NewInMemoryTokenStore()
	tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "expired",
		SpotifyRefreshToken: "refresh",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour),
	})

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenEndpoint: mock.URL,
	})

	token, err := refresher.GetAccessToken(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", token, "should return the refreshed token")
}

func TestSpotifyTokenRefreshFailureReturnsError(t *testing.T) {
	mock := mockSpotifyTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})

	tokenStore := store.NewInMemoryTokenStore()
	tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "expired",
		SpotifyRefreshToken: "bad-refresh",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour),
	})

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenEndpoint: mock.URL,
	})

	_, err := refresher.GetAccessToken(context.Background(), "client-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestSpotifyTokenRefreshNoThunderingHerd(t *testing.T) {
	var refreshCount atomic.Int32
	mock := mockSpotifyTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCount.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate latency
		defaultMockHandler()(w, r)
	})

	tokenStore := store.NewInMemoryTokenStore()
	tokenStore.Store(context.Background(), "client-1", &store.TokenRecord{
		SpotifyAccessToken:  "expired",
		SpotifyRefreshToken: "refresh",
		SpotifyTokenExpiry:  time.Now().Add(-time.Hour),
	})

	refresher := NewTokenRefresher(tokenStore, &SpotifyClient{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenEndpoint: mock.URL,
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := refresher.GetAccessToken(context.Background(), "client-1")
			assert.NoError(t, err)
			assert.Equal(t, "refreshed-access-token", token)
		}()
	}
	wg.Wait()

	// singleflight should coalesce concurrent refreshes into one call
	assert.Equal(t, int32(1), refreshCount.Load(),
		"concurrent requests for same client should trigger only one refresh")
}
