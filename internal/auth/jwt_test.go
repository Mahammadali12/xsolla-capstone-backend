package auth

import (
	"testing"
	"time"
)

func TestRefreshTokensAreAlwaysNew(t *testing.T) {
	secret := []byte("refresh-test-secret")
	a, err := RefreshToken(7, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RefreshToken(7, secret, time.Hour) // same user, same second
	if a == b {
		t.Fatal("two refresh tokens made in the same second are identical")
	}
	for _, tok := range []string{a, b} {
		claims, err := Verify(tok, secret)
		if err != nil || claims.UserID != 7 || claims.ID == "" {
			t.Errorf("refresh token does not verify: %+v, %v", claims, err)
		}
	}
}

func TestAccessTokensKeepTheirShape(t *testing.T) {
	secret := []byte("access-test-secret")
	tok, err := AccessToken(9, []string{"student"}, secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify(tok, secret)
	if err != nil || claims.UserID != 9 || len(claims.Roles) != 1 || claims.ID != "" {
		t.Errorf("access token: %+v, %v (no jti expected)", claims, err)
	}
}
