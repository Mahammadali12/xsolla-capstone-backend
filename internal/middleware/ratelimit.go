package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limiter is an in-memory token bucket per key (a client IP or an account). Each key may spend up to Burst requests
// at once; one more becomes available every Every. Buckets that have refilled completely are dropped now and then,
// so the map only holds keys that were busy recently.
//
// The state lives in this process: with several API replicas each one limits on its own.
type Limiter struct {
	Burst int
	Every time.Duration

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter allows burst requests at once per key, then one every `every`.
func NewLimiter(burst int, every time.Duration) *Limiter {
	return &Limiter{Burst: burst, Every: every, buckets: map[string]*bucket{}, now: time.Now}
}

// refill brings the key's bucket up to date and returns it (a new key starts full). Call with mu held.
func (l *Limiter) refill(key string, now time.Time) *bucket {
	if now.Sub(l.lastSweep) > time.Minute {
		full := time.Duration(l.Burst) * l.Every
		for k, b := range l.buckets {
			if now.Sub(b.at) >= full {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.Burst), at: now}
		l.buckets[key] = b
		return b
	}
	b.tokens = math.Min(float64(l.Burst), b.tokens+float64(now.Sub(b.at))/float64(l.Every))
	b.at = now
	return b
}

// wait is how long until the bucket holds one whole request. Call with mu held.
func (l *Limiter) wait(b *bucket) time.Duration {
	if b.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - b.tokens) * float64(l.Every))
}

// Allow spends one request for key. When none is left it spends nothing and says how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, l.now())
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, l.wait(b)
}

// Check says whether key has a request left without spending it.
func (l *Limiter) Check(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, l.now())
	return b.tokens >= 1, l.wait(b)
}

// Spend uses one request for key, e.g. after a failed login.
func (l *Limiter) Spend(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, l.now())
	b.tokens = math.Max(0, b.tokens-1)
}

// ClientIP is the address of the client that sent r. Behind a reverse proxy (the k8s ingress) every request comes from
// the proxy, so with trustProxy the proxy's headers are used instead: X-Real-IP, else the last X-Forwarded-For entry
// (the one the nearest proxy added). Only turn trustProxy on behind a proxy that sets them, or clients can make up
// their own address.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// tooManyRequests answers 429 in the API's usual error shape, with Retry-After in whole seconds.
func tooManyRequests(w http.ResponseWriter, wait time.Duration, what string) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", fmt.Sprint(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "too_many_requests",
		"message": fmt.Sprintf("Too many %s. Try again in %d seconds.", what, secs),
		"status":  http.StatusTooManyRequests,
	})
}

// LimitByIP lets each client address through l; the rest get 429. what names the requests in the message
// ("sign-in attempts").
func LimitByIP(l *Limiter, trustProxy bool, what string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(ClientIP(r, trustProxy)); !ok {
			tooManyRequests(w, wait, what)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxLoginPeek is how much of a login body is read to find the account; a real one is far smaller.
const maxLoginPeek = 64 << 10

// LimitFailedLogins slows down password guessing on one account, from however many addresses: only failed sign-ins
// (401) spend from the account's bucket, and when it is empty that account gets 429 until a request is available
// again, even with the right password. Successful sign-ins never count.
func LimitFailedLogins(l *Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxLoginPeek+1))
		if err != nil || len(body) > maxLoginPeek {
			next.ServeHTTP(w, r) // let the handler refuse it as it always does
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var req struct {
			Login string `json:"login"`
		}
		_ = json.Unmarshal(body, &req)
		account := strings.ToLower(strings.TrimSpace(req.Login))
		if account == "" {
			next.ServeHTTP(w, r)
			return
		}
		if ok, wait := l.Check(account); !ok {
			tooManyRequests(w, wait, "failed sign-ins for this account")
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status == http.StatusUnauthorized {
			l.Spend(account)
		}
	})
}
