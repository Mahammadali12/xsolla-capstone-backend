package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetireRefreshTokenKeepsAShortGrace(t *testing.T) {
	db := testDatabase(t)
	st := NewStore(db)
	ctx := context.Background()
	user := insertTestUser(t, db, fmt.Sprintf("refresh-grace-%d@example.com", time.Now().UnixNano()), "Refresh Grace")

	long, short := fmt.Sprintf("long-%d", time.Now().UnixNano()), fmt.Sprintf("short-%d", time.Now().UnixNano())
	if err := st.SaveRefreshToken(ctx, user, long, time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRefreshToken(ctx, user, short, time.Now().Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := st.RetireRefreshToken(ctx, long, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	_, _, exp, err := st.FindRefreshTokenByHash(ctx, long)
	if err != nil {
		t.Fatalf("a token in its grace period must still be found: %v", err)
	}
	if left := time.Until(exp); left > 2*time.Minute+5*time.Second || left < time.Minute {
		t.Errorf("after retiring, the 7-day token has %v left, want about 2 minutes", left)
	}

	if err := st.RetireRefreshToken(ctx, short, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, exp, _ := st.FindRefreshTokenByHash(ctx, short); time.Until(exp) > 31*time.Second {
		t.Errorf("retiring must never make a token live longer: %v left, want at most 30s", time.Until(exp))
	}

	if err := st.RetireRefreshToken(ctx, long, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.FindRefreshTokenByHash(ctx, long); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Errorf("after the grace period: %v, want ErrRefreshTokenNotFound", err)
	}
}
