package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const defaultSpotifyTokenEndpoint = "https://accounts.spotify.com/api/token"

// SpotifyClient handles OAuth token exchange and refresh with Spotify.
type SpotifyClient struct {
	ClientID      string
	ClientSecret  string
	TokenEndpoint string
	HTTPClient    *http.Client
}

// SpotifyTokenResponse represents Spotify's token endpoint response.
type SpotifyTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// ExchangeCode exchanges an authorization code for tokens at Spotify's token endpoint.
func (c *SpotifyClient) ExchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*SpotifyTokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"code_verifier": {codeVerifier},
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	endpoint := c.TokenEndpoint
	if endpoint == "" {
		endpoint = defaultSpotifyTokenEndpoint
	}

	resp, err := httpClient.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("spotify token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading spotify token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spotify token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp SpotifyTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing spotify token response: %w", err)
	}

	return &tokenResp, nil
}
