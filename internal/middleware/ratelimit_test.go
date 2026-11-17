package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rlClock is a clock the test moves by hand.
type rlClock struct{ t time.Time }

func (c *rlClock) now() time.Time      { return c.t }
func (c *rlClock) add(d time.Duration) { c.t = c.t.Add(d) }
func rlLimiter(burst int, every time.Duration) (*Limiter, *rlClock) {
	c := &rlClock{t: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
	l := NewLimiter(burst, every)
	l.now = c.now
	return l, c
}

func TestLimiterBurstThenRefill(t *testing.T) {
	l, clock := rlLimiter(3, 10*time.Second)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d of the burst refused", i+1)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != 10*time.Second {
		t.Fatalf("4th: ok=%v wait=%v, want refused and 10s", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another key has its own bucket")
	}
	clock.add(4 * time.Second)
	if ok, wait := l.Allow("a"); ok || wait != 6*time.Second {
		t.Fatalf("after 4s: ok=%v wait=%v, want refused and 6s left", ok, wait)
	}
	clock.add(6 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("after 10s one request is back")
	}
	clock.add(time.Hour)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("after an hour the bucket holds only the burst, request %d refused", i+1)
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("the bucket never holds more than the burst")
	}
}

func TestLimiterForgetsIdleKeys(t *testing.T) {
	l, clock := rlLimiter(2, time.Second)
	l.Allow("old")
	clock.add(2 * time.Minute)
	l.Allow("new")
	if _, ok := l.buckets["old"]; ok || len(l.buckets) != 1 {
		t.Fatalf("buckets kept: %v", l.buckets)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/login", nil)
	r.RemoteAddr = "10.0.0.7:51234"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := ClientIP(r, false); got != "10.0.0.7" {
		t.Errorf("not trusting the proxy: %q, want the connection's address", got)
	}
	if got := ClientIP(r, true); got != "203.0.113.9" {
		t.Errorf("trusting the proxy: %q, want the last X-Forwarded-For entry (the one the proxy added)", got)
	}
	r.Header.Set("X-Real-IP", "198.51.100.4")
	if got := ClientIP(r, true); got != "198.51.100.4" {
		t.Errorf("X-Real-IP first: %q", got)
	}
}

func rlHit(h http.Handler, ip, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	r.RemoteAddr = ip + ":40000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLimitByIP(t *testing.T) {
	l, clock := rlLimiter(2, 3*time.Second)
	calls := 0
	h := LimitByIP(l, false, "sign-in attempts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	rlHit(h, "1.1.1.1", "")
	rlHit(h, "1.1.1.1", "")
	w := rlHit(h, "1.1.1.1", "")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 429 || w.Header().Get("Retry-After") != "3" || body["error"] != "too_many_requests" ||
		body["message"] != "Too many sign-in attempts. Try again in 3 seconds." || calls != 2 {
		t.Fatalf("3rd: %d %v %s (handler ran %d times)", w.Code, w.Header(), w.Body, calls)
	}
	if w := rlHit(h, "2.2.2.2", ""); w.Code != 200 {
		t.Errorf("another address: %d", w.Code)
	}
	clock.add(3 * time.Second)
	if w := rlHit(h, "1.1.1.1", ""); w.Code != 200 {
		t.Errorf("after waiting: %d", w.Code)
	}
}

// rlLogin answers 401 unless the password is "right", and checks the body still reaches it.
func rlLogin(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Login, Password string }
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &req); err != nil || req.Login == "" {
			t.Errorf("the handler did not get the body: %q", raw)
		}
		if req.Password != "right" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
}

func TestLimitFailedLogins(t *testing.T) {
	l, clock := rlLimiter(5, time.Minute)
	h := LimitFailedLogins(l, rlLogin(t))
	wrong := `{"login":"anna@example.com","password":"guess"}`
	right := `{"login":" Anna@Example.com ","password":"right"}`

	for i := 0; i < 10; i++ { // successful sign-ins never count
		if w := rlHit(h, "1.1.1.1", right); w.Code != 200 {
			t.Fatalf("sign-in %d: %d", i+1, w.Code)
		}
	}
	for i := 0; i < 5; i++ { // five wrong passwords, from different addresses
		if w := rlHit(h, "10.0.0."+string(rune('1'+i)), wrong); w.Code != 401 {
			t.Fatalf("wrong password %d: %d, want 401", i+1, w.Code)
		}
	}
	w := rlHit(h, "1.1.1.1", right)
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" || !strings.Contains(w.Body.String(), "failed sign-ins for this account") {
		t.Fatalf("after 5 wrong passwords even the right one waits: %d %s", w.Code, w.Body)
	}
	if w := rlHit(h, "1.1.1.1", `{"login":"bek@example.com","password":"right"}`); w.Code != 200 {
		t.Errorf("another account is not affected: %d", w.Code)
	}
	clock.add(time.Minute)
	if w := rlHit(h, "1.1.1.1", right); w.Code != 200 {
		t.Errorf("a minute later: %d", w.Code)
	}
	if w := rlHit(h, "1.1.1.1", wrong); w.Code != 401 {
		t.Errorf("one more try: %d", w.Code)
	}
	if w := rlHit(h, "1.1.1.1", wrong); w.Code != 429 {
		t.Errorf("then wait again: %d", w.Code)
	}
}

func TestLimitFailedLoginsLeavesOddBodiesToTheHandler(t *testing.T) {
	l, _ := rlLimiter(1, time.Minute)
	calls := 0
	h := LimitFailedLogins(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	for _, body := range []string{"not json", `{"password":"x"}`, strings.Repeat("a", maxLoginPeek+10)} {
		if w := rlHit(h, "1.1.1.1", body); w.Code != 400 {
			t.Errorf("%.20q: %d, want the handler's own 400", body, w.Code)
		}
	}
	if calls != 3 {
		t.Errorf("handler ran %d times, want 3", calls)
	}
}
