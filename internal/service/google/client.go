package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Service provides access to stored Google OAuth refresh tokens for a user.
type Service interface {
	// GetValidAccessToken returns a usable access token for the user,
	// refreshing via the stored refresh token if needed.
	GetValidAccessToken(ctx context.Context, userID int64) (string, error)
}

// Client is an authenticated caller for Google APIs.
// It holds an access token and automatically refreshes it on 401 responses.
type Client struct {
	httpClient *http.Client
	baseURL    string

	svc       Service
	userID    int64
	accessToken string
}

// NewClient creates a Google API client for the given user.
// It does not validate the access token immediately; the first API call
// will refresh it if needed.
func NewClient(userID int64, svc Service, accessToken string) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL:     "https://www.googleapis.com",
		svc:         svc,
		userID:      userID,
		accessToken: accessToken,
	}
}

// do performs an HTTP request with Bearer auth and auto-refresh on 401.
// On a 401 response, it refreshes the access token once and retries.
func (c *Client) do(method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.doWithRetry(req, v)
}

// doWithRetry executes the request with bearer auth and auto-refresh on 401.
func (c *Client) doWithRetry(req *http.Request, v any) error {
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("google API %s %s failed: %s", req.Method, req.URL.Path, resp.Status)
		}
		if v != nil {
			return json.NewDecoder(resp.Body).Decode(v)
		}
		return nil
	}
	resp.Body.Close()

	// Refresh the access token and retry once.
	newToken, err := c.svc.GetValidAccessToken(req.Context(), c.userID)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}

	c.accessToken = newToken
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google API %s %s failed after refresh: %s", req.Method, req.URL.Path, resp.Status)
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}
