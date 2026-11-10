package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	keysSessionCookie = "go_sites_keys_session"
	sessionLifetime   = 7 * 24 * time.Hour
	maxLoginFailures  = 5
	loginLockDuration = time.Minute
)

type authManager struct {
	passwordHash []byte
	signingKey   []byte
	origin       string
	cookieSecure bool
	limiter      loginLimiter
}

type loginLimiter struct {
	mu          sync.Mutex
	failures    int
	blockedTill time.Time
}

func newAuthManager(hash, sessionSecret []byte, origin string, cookieSecure bool) (*authManager, error) {
	if len(hash) == 0 {
		return nil, errors.New("administrator password hash is missing")
	}
	if _, err := bcrypt.Cost(hash); err != nil {
		return nil, fmt.Errorf("parse administrator password hash: %w", err)
	}
	if len(sessionSecret) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes")
	}
	tagInput := append(append([]byte(nil), sessionSecret...), hash...)
	tag := sha256.Sum256(tagInput)
	return &authManager{
		passwordHash: append([]byte(nil), hash...),
		signingKey:   append([]byte(nil), tag[:]...),
		origin:       strings.TrimRight(origin, "/"),
		cookieSecure: cookieSecure,
	}, nil
}

func (a *authManager) verifyPassword(password string) bool {
	if len(password) > 4096 {
		return false
	}
	return bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)) == nil
}

func (a *authManager) allowAttempt() bool {
	a.limiter.mu.Lock()
	defer a.limiter.mu.Unlock()
	now := time.Now()
	if now.Before(a.limiter.blockedTill) {
		return false
	}
	if !a.limiter.blockedTill.IsZero() && now.Sub(a.limiter.blockedTill) > loginLockDuration {
		a.limiter.failures = 0
		a.limiter.blockedTill = time.Time{}
	}
	return true
}

func (a *authManager) recordFailure() {
	a.limiter.mu.Lock()
	defer a.limiter.mu.Unlock()
	a.limiter.failures++
	if a.limiter.failures >= maxLoginFailures {
		a.limiter.blockedTill = time.Now().Add(loginLockDuration)
		a.limiter.failures = 0
	}
}

func (a *authManager) recordSuccess() {
	a.limiter.mu.Lock()
	defer a.limiter.mu.Unlock()
	a.limiter.failures = 0
	a.limiter.blockedTill = time.Time{}
}

func (a *authManager) newSessionToken(now time.Time) (string, error) {
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	expires := now.Add(sessionLifetime).Unix()
	payload := strconv.FormatInt(expires, 10) + "." + base64.RawURLEncoding.EncodeToString(nonce)
	signature := a.sessionMAC("session:" + payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (a *authManager) verifySessionToken(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expires < now.Unix() || expires > now.Add(sessionLifetime+time.Minute).Unix() {
		return false
	}
	expected := a.sessionMAC("session:" + parts[0] + "." + parts[1])
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, expected) {
		return false
	}
	return true
}

func (a *authManager) csrfToken(token string) string {
	value := a.sessionMAC("csrf:" + token)
	return base64.RawURLEncoding.EncodeToString(value)
}

func (a *authManager) verifyCSRF(token, supplied string) bool {
	expected := []byte(a.csrfToken(token))
	got := []byte(strings.TrimSpace(supplied))
	return len(got) > 0 && hmac.Equal(got, expected)
}

func (a *authManager) sessionMAC(value string) []byte {
	mac := hmac.New(sha256.New, a.signingKey)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (a *authManager) sessionFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(keysSessionCookie)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, a.verifySessionToken(cookie.Value, time.Now())
}

func (a *authManager) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     keysSessionCookie,
		Value:    token,
		Path:     "/keys",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
		Expires:  time.Now().Add(time.Duration(maxAge) * time.Second),
	})
}

func (a *authManager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     keysSessionCookie,
		Value:    "",
		Path:     "/keys",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (a *authManager) validOrigin(r *http.Request) bool {
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	return origin == "" || strings.EqualFold(origin, a.origin)
}
