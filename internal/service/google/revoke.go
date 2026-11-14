package google

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// revokeEndpoint is Google's OAuth token revocation endpoint.
const revokeEndpoint = "https://oauth2.googleapis.com/revoke"

// RevokeToken revokes a Google refresh token at Google. Revoking the refresh
// token also invalidates the access tokens issued from it.
func RevokeToken(ctx context.Context, token string) error {
	return revokeTokenAt(ctx, revokeEndpoint, token)
}

// revokeTokenAt is RevokeToken with an explicit endpoint so tests can use a
// local server.
func revokeTokenAt(ctx context.Context, endpoint, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 400 invalid_token means the token is already expired or revoked: the
	// caller wanted it gone and it is gone.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest {
		return nil
	}
	return fmt.Errorf("google token revoke failed: %s", resp.Status)
}
