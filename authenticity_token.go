package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

const authenticityTokenLength = 32

// AuthenticityTokenSessionKey is the key used to store the session token in Session.Data.
const AuthenticityTokenSessionKey = "authenticity_token"

// GenerateAuthenticityToken creates a new cryptographically random per-session CSRF token.
func GenerateAuthenticityToken() (string, error) {
	raw := make([]byte, authenticityTokenLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// CreateAuthenticityToken creates a one-time masked token from the session token for embedding in forms.
func CreateAuthenticityToken(token string) string {
	sessionToken, err := base64.StdEncoding.DecodeString(token)
	if err != nil || len(sessionToken) != authenticityTokenLength {
		return ""
	}

	mask := make([]byte, authenticityTokenLength)
	if _, err := rand.Read(mask); err != nil {
		return ""
	}

	masked := make([]byte, authenticityTokenLength)
	for i := range sessionToken {
		masked[i] = sessionToken[i] ^ mask[i]
	}

	combined := make([]byte, authenticityTokenLength*2)
	copy(combined[:authenticityTokenLength], mask)
	copy(combined[authenticityTokenLength:], masked)

	return base64.StdEncoding.EncodeToString(combined)
}

// VerifyAuthenticityToken checks a masked form token against the session token.
func VerifyAuthenticityToken(sessionToken, formToken string) bool {
	rawSession, err := base64.StdEncoding.DecodeString(sessionToken)
	if err != nil || len(rawSession) != authenticityTokenLength {
		return false
	}

	rawForm, err := base64.StdEncoding.DecodeString(formToken)
	if err != nil || len(rawForm) != authenticityTokenLength*2 {
		return false
	}

	mask := rawForm[:authenticityTokenLength]
	masked := rawForm[authenticityTokenLength:]

	candidate := make([]byte, authenticityTokenLength)
	for i := range mask {
		candidate[i] = masked[i] ^ mask[i]
	}

	return subtle.ConstantTimeCompare(rawSession, candidate) == 1
}

type VerifyAuthenticityTokenConfig struct {
	TokenErrorHandler func(w http.ResponseWriter, r *http.Request)
}

func VerifyAuthenticityTokenMiddleware(options ...func(*VerifyAuthenticityTokenConfig)) func(http.Handler) http.Handler {
	cfg := VerifyAuthenticityTokenConfig{
		TokenErrorHandler: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		},
	}
	for _, option := range options {
		option(&cfg)
	}

	methodsToVerify := map[string]struct{}{
		http.MethodPost:   {},
		http.MethodPut:    {},
		http.MethodPatch:  {},
		http.MethodDelete: {},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, shouldVerify := methodsToVerify[r.Method]; shouldVerify {
				session, ok := SessionFromContext(r.Context())
				if !ok {
					cfg.TokenErrorHandler(w, r)
					return
				}
				sessionToken, _ := session.Data[AuthenticityTokenSessionKey].(string)

				formToken := r.Header.Get("X-CSRF-Token")
				if formToken == "" {
					formToken = r.FormValue("authenticity_token")
				}

				if !VerifyAuthenticityToken(sessionToken, formToken) {
					cfg.TokenErrorHandler(w, r)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
