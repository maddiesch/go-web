package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	ErrSignedCookieNotFound = errors.New("signed cookie not found")
	ErrSignedCookieInvalid  = errors.New("signed cookie invalid")
	ErrWeakKey              = errors.New("signing key too short")
)

// minSigningKeyLength is the minimum acceptable HMAC key size, matching the
// SHA-256 output size. Shorter keys weaken the signature and are rejected.
const minSigningKeyLength = 32

// SignedCookieConfig holds cookie identity and signing parameters.
//
// Cookies are signed, not encrypted: the value is tamper-proof but readable by
// anyone who holds the cookie. Never store secrets in a session you persist
// here — the CSRF token is safe because it isn't a cross-site bearer secret.
//
// There is no key rotation: changing Key invalidates every existing cookie.
type SignedCookieConfig struct {
	Name string
	// Key signs the cookie via HMAC-SHA256 and must be at least 32 bytes.
	Key      []byte
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	SameSite http.SameSite
}

// WriteSignedCookie signs value and sets an HttpOnly cookie on w.
// value is stored as-is; callers are responsible for any encoding.
func WriteSignedCookie(w http.ResponseWriter, cfg SignedCookieConfig, value string) error {
	signed, err := signCookieValue(cfg.Name, cfg.Key, value)
	if err != nil {
		return err
	}
	http.SetCookie(w, newSignedCookie(cfg, signed))
	return nil
}

// ReadSignedCookie reads and verifies a signed cookie from r.
// Returns ErrSignedCookieNotFound if absent, ErrSignedCookieInvalid if tampered.
func ReadSignedCookie(r *http.Request, cfg SignedCookieConfig) (string, error) {
	cookie, err := r.Cookie(cfg.Name)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", ErrSignedCookieNotFound
		}
		return "", err
	}
	return verifyCookieValue(cfg.Name, cfg.Key, cookie.Value)
}

// DeleteSignedCookie expires the signed cookie immediately.
func DeleteSignedCookie(w http.ResponseWriter, cfg SignedCookieConfig) {
	c := newSignedCookie(cfg, "")
	c.MaxAge = -1
	c.Expires = time.Unix(0, 0)
	http.SetCookie(w, c)
}

func newSignedCookie(cfg SignedCookieConfig, value string) *http.Cookie {
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	sameSite := cfg.SameSite
	if sameSite == 0 {
		sameSite = http.SameSiteLaxMode
	}
	return &http.Cookie{
		Name:     cfg.Name,
		Value:    value,
		Path:     path,
		Domain:   cfg.Domain,
		MaxAge:   cfg.MaxAge,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: sameSite,
	}
}

// signCookieValue produces value|base64url(hmac(name|value)).
// The HMAC covers both the cookie name and value to prevent substitution attacks.
func signCookieValue(name string, key []byte, value string) (string, error) {
	sig, err := cookieHMAC(name, key, value)
	if err != nil {
		return "", err
	}
	return value + "." + sig, nil
}

func verifyCookieValue(name string, key []byte, raw string) (string, error) {
	// Split on the last "." — the signature is base64url and never contains a
	// dot, so this is robust even when value itself does.
	i := strings.LastIndex(raw, ".")
	if i < 0 {
		return "", ErrSignedCookieInvalid
	}
	value, sig := raw[:i], raw[i+1:]
	expected, err := cookieHMAC(name, key, value)
	if err != nil {
		return "", err
	}
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", ErrSignedCookieInvalid
	}
	return value, nil
}

func cookieHMAC(name string, key []byte, value string) (string, error) {
	if len(key) < minSigningKeyLength {
		return "", ErrWeakKey
	}
	mac := hmac.New(sha256.New, key)
	if _, err := mac.Write([]byte(name + "|" + value)); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Session is the structured value stored in the session cookie.
//
// Data is signed but not encrypted — do not store secrets in it.
type Session struct {
	ID string `json:"id"`
	// ExpiresAt bounds server-side validity. A zero value never expires, so
	// set it unless you intend a session that only ends with the cookie.
	ExpiresAt time.Time      `json:"expires_at,omitzero"`
	Data      map[string]any `json:"data,omitempty"`
}

// Valid returns true when the session has a non-empty ID and hasn't expired.
// A zero ExpiresAt is treated as never-expiring.
func (s *Session) Valid() bool {
	if s == nil || s.ID == "" {
		return false
	}
	return s.ExpiresAt.IsZero() || time.Now().Before(s.ExpiresAt)
}

type sessionContextKey struct{}

// SessionFromContext retrieves the validated Session set by SessionMiddleware.
// Returns nil, false when no valid session is present.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionContextKey{}).(*Session)
	return s, ok && s != nil
}

// WriteSession JSON-encodes session, base64-encodes it, and writes it as a signed cookie.
func WriteSession(w http.ResponseWriter, cfg SignedCookieConfig, session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return WriteSignedCookie(w, cfg, base64.RawURLEncoding.EncodeToString(data))
}

// DeleteSession expires the session cookie.
func DeleteSession(w http.ResponseWriter, cfg SignedCookieConfig) {
	DeleteSignedCookie(w, cfg)
}

// SessionOptions configures SessionMiddleware.
type SessionOptions struct {
	Cookie SignedCookieConfig
}

// WithSignedCookie sets the cookie config used to read the session.
func WithSignedCookie(cfg SignedCookieConfig) func(*SessionOptions) {
	return func(o *SessionOptions) {
		o.Cookie = cfg
	}
}

// SessionMiddleware reads the session cookie, decodes it, validates it, and
// injects a *Session into the request context via sessionContextKey.
// Requests with no cookie or an invalid/expired session still pass through —
// use SessionFromContext to distinguish.
func SessionMiddleware(options ...func(*SessionOptions)) func(http.Handler) http.Handler {
	var cfg SessionOptions
	for _, option := range options {
		option(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if encoded, err := ReadSignedCookie(r, cfg.Cookie); err == nil {
				if data, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
					var session Session
					if json.Unmarshal(data, &session) == nil && session.Valid() {
						r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, &session))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
