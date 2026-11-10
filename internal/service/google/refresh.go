package google

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// tokenExchangeResponse is the response from Google's OAuth token endpoint.
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// ExchangeAuthorizationCode exchanges an authorization code for Google OAuth tokens.
func ExchangeAuthorizationCode(clientID, clientSecret, redirectURI, code string) (*tokenExchangeResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token exchange failed: %s", resp.Status)
	}

	var tokens tokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

// ExchangeRefreshToken exchanges a Google refresh token for a new access token.
func ExchangeRefreshToken(clientID, clientSecret, refreshToken string) (*tokenExchangeResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token refresh failed: %s", resp.Status)
	}

	var tokens tokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}
