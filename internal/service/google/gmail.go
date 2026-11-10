package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)
// SendEmail sends an email via the Gmail API.
// The message is a plain-text RFC 822 message with To, Subject, and body.
func (c *Client) SendEmail(ctx context.Context, to, subject, body string) (string, error) {
	raw := buildRFC822(to, subject, body)
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))

	payload := fmt.Sprintf(`{"raw":"%s"}`, encoded)
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/gmail/v1/users/me/messages.send", strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var result struct {
		ID string `json:"id"`
	}
	if err := c.doWithRetry(req, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// buildRFC822 constructs a minimal RFC 822 email message.
func buildRFC822(to, subject, body string) string {
	var sb strings.Builder
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}
