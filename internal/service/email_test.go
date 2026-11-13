package service

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{name: "simple", email: "a@b.co"},
		{name: "plus tag", email: "first.last+tag@example.com"},
		{name: "uppercase", email: "USER@Example.COM"},
		{name: "empty", email: "", wantErr: true},
		{name: "no at", email: "foo", wantErr: true},
		{name: "no dot in domain", email: "a@b", wantErr: true},
		{name: "no local part", email: "@b.co", wantErr: true},
		{name: "no domain", email: "a@.co", wantErr: true},
		{name: "space in local part", email: "a b@c.co", wantErr: true},
		{name: "space in domain", email: "a@b c.co", wantErr: true},
		{name: "two at signs", email: "a@@b.co", wantErr: true},
		{name: "local part too long", email: strings.Repeat("a", 65) + "@b.co", wantErr: true},
		{name: "total too long", email: strings.Repeat("a", 64) + "@" + strings.Repeat("b", 190) + ".co", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmail(tc.email)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error for %q", tc.email)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected %q to be valid, got %v", tc.email, err)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := normalizeEmail("  USER@Example.COM \t"); got != "user@example.com" {
		t.Fatalf("expected a trimmed lowercase address, got %q", got)
	}
}
