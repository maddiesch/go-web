package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAuthenticityToken(t *testing.T) {
	t.Run("returns a non-empty token", func(t *testing.T) {
		tok, err := GenerateAuthenticityToken()
		require.NoError(t, err)
		assert.NotEmpty(t, tok)
	})

	t.Run("each call returns a unique token", func(t *testing.T) {
		a, err := GenerateAuthenticityToken()
		require.NoError(t, err)
		b, err := GenerateAuthenticityToken()
		require.NoError(t, err)
		assert.NotEqual(t, a, b)
	})
}

func TestCreateAuthenticityToken(t *testing.T) {
	session, err := GenerateAuthenticityToken()
	require.NoError(t, err)

	t.Run("returns a non-empty masked token", func(t *testing.T) {
		form := CreateAuthenticityToken(session)
		assert.NotEmpty(t, form)
	})

	t.Run("each call produces a unique masked token", func(t *testing.T) {
		a := CreateAuthenticityToken(session)
		b := CreateAuthenticityToken(session)
		assert.NotEqual(t, a, b)
	})

	t.Run("returns empty string for invalid session token", func(t *testing.T) {
		assert.Empty(t, CreateAuthenticityToken("not-valid-base64!!!"))
		assert.Empty(t, CreateAuthenticityToken(""))
	})
}

func TestVerifyAuthenticityToken(t *testing.T) {
	session, err := GenerateAuthenticityToken()
	require.NoError(t, err)

	t.Run("valid masked token verifies", func(t *testing.T) {
		form := CreateAuthenticityToken(session)
		assert.True(t, VerifyAuthenticityToken(session, form))
	})

	t.Run("multiple masked tokens from same session all verify", func(t *testing.T) {
		for range 5 {
			form := CreateAuthenticityToken(session)
			assert.True(t, VerifyAuthenticityToken(session, form))
		}
	})

	t.Run("wrong session token fails", func(t *testing.T) {
		other, err := GenerateAuthenticityToken()
		require.NoError(t, err)
		form := CreateAuthenticityToken(session)
		assert.False(t, VerifyAuthenticityToken(other, form))
	})

	t.Run("tampered form token fails", func(t *testing.T) {
		assert.False(t, VerifyAuthenticityToken(session, "dGhpcyBpcyB0b3RhbGx5IGZha2UhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEhISEh"))
	})

	t.Run("invalid inputs fail gracefully", func(t *testing.T) {
		assert.False(t, VerifyAuthenticityToken("", ""))
		assert.False(t, VerifyAuthenticityToken("not-base64!!!", "also-not-base64!!!"))
		assert.False(t, VerifyAuthenticityToken(session, ""))
	})
}

func TestVerifyAuthenticityTokenMiddleware(t *testing.T) {
	sessionToken, err := GenerateAuthenticityToken()
	require.NoError(t, err)

	sessionWithToken := &Session{
		ID:   "test-session",
		Data: map[string]any{AuthenticityTokenSessionKey: sessionToken},
	}

	ctxWithSession := context.WithValue(context.Background(), sessionContextKey{}, sessionWithToken)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := VerifyAuthenticityTokenMiddleware()
	handler := middleware(okHandler)

	safeMethods := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	for _, method := range safeMethods {
		t.Run("passes safe method "+method+" without token", func(t *testing.T) {
			r := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

	unsafeMethods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, method := range unsafeMethods {
		t.Run("rejects "+method+" with no session", func(t *testing.T) {
			r := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})

		t.Run("rejects "+method+" with missing token", func(t *testing.T) {
			r := httptest.NewRequest(method, "/", nil)
			r = r.WithContext(ctxWithSession)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})

		t.Run("accepts "+method+" with valid X-CSRF-Token header", func(t *testing.T) {
			formToken := CreateAuthenticityToken(sessionToken)
			r := httptest.NewRequest(method, "/", nil)
			r.Header.Set("X-CSRF-Token", formToken)
			r = r.WithContext(ctxWithSession)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("rejects "+method+" with wrong token", func(t *testing.T) {
			other, err := GenerateAuthenticityToken()
			require.NoError(t, err)
			formToken := CreateAuthenticityToken(other)
			r := httptest.NewRequest(method, "/", nil)
			r.Header.Set("X-CSRF-Token", formToken)
			r = r.WithContext(ctxWithSession)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}

	// Go only parses form bodies for POST, PUT, PATCH — not DELETE
	formMethods := []string{http.MethodPost, http.MethodPut, http.MethodPatch}
	for _, method := range formMethods {
		t.Run("accepts "+method+" with valid form body token", func(t *testing.T) {
			formToken := CreateAuthenticityToken(sessionToken)
			body := url.Values{"authenticity_token": {formToken}}.Encode()
			r := httptest.NewRequest(method, "/", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r = r.WithContext(ctxWithSession)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

	t.Run("custom TokenErrorHandler is called on failure", func(t *testing.T) {
		var handlerCalled bool
		customHandler := VerifyAuthenticityTokenMiddleware(func(cfg *VerifyAuthenticityTokenConfig) {
			cfg.TokenErrorHandler = func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				http.Error(w, "nope", http.StatusTeapot)
			}
		})(okHandler)

		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r = r.WithContext(ctxWithSession)
		w := httptest.NewRecorder()
		customHandler.ServeHTTP(w, r)

		assert.True(t, handlerCalled)
		assert.Equal(t, http.StatusTeapot, w.Code)
	})

	t.Run("custom TokenErrorHandler is not called on success", func(t *testing.T) {
		var handlerCalled bool
		customHandler := VerifyAuthenticityTokenMiddleware(func(cfg *VerifyAuthenticityTokenConfig) {
			cfg.TokenErrorHandler = func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				http.Error(w, "nope", http.StatusTeapot)
			}
		})(okHandler)

		formToken := CreateAuthenticityToken(sessionToken)
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-CSRF-Token", formToken)
		r = r.WithContext(ctxWithSession)
		w := httptest.NewRecorder()
		customHandler.ServeHTTP(w, r)

		assert.False(t, handlerCalled)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
