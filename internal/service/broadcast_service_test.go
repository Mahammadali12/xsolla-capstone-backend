package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type bcStore struct {
	sent []string
}

func (f *bcStore) CreateBroadcast(_ context.Context, message, link, audience string, sentBy int64) (*store.Broadcast, error) {
	f.sent = append(f.sent, message, link, audience)
	return &store.Broadcast{ID: 1, Message: message, Link: link, Audience: audience, SentBy: &sentBy}, nil
}

func (f *bcStore) ListBroadcasts(context.Context) ([]store.Broadcast, error) { return nil, nil }

func TestSendBroadcast(t *testing.T) {
	st := &bcStore{}
	if _, err := NewBroadcastService(st).Send(context.Background(), 1, "  No lectures on Friday.  ", " /calendar ", "all"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(st.sent, "|") != "No lectures on Friday.|/calendar|all" {
		t.Errorf("stored %q", st.sent)
	}

	cases := []struct {
		name, message, link, audience string
		want                          string
	}{
		{"no link is fine", "Hi", "", "students", ""},
		{"empty message", "  ", "", "all", "message"},
		{"too long", strings.Repeat("a", 501), "", "all", "message"},
		{"500 letters (not bytes) are fine", strings.Repeat("ə", 500), "", "all", ""},
		{"another site", "Hi", "https://evil.example", "all", "link"},
		{"protocol-relative", "Hi", "//evil.example", "all", "link"},
		{"spaces in the link", "Hi", "/cal endar", "all", "link"},
		{"unknown audience", "Hi", "", "parents", "audience"},
		{"everything wrong", "", "x", "admins", "message,link,audience"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewBroadcastService(&bcStore{}).Send(context.Background(), 1, c.message, c.link, c.audience)
			var invalid *BroadcastInvalidError
			got := []string{}
			if errors.As(err, &invalid) {
				for _, f := range invalid.Fields {
					got = append(got, f.Field)
				}
			}
			if strings.Join(got, ",") != c.want || (c.want == "" && err != nil) {
				t.Errorf("fields %v (err %v), want %q", got, err, c.want)
			}
		})
	}
}
