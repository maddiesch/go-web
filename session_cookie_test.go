package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	web "github.com/maddiesch/go-web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSigningKey = []byte("super-secret-test-key-32-bytes!!")

func testSignedCookieConfig() web.SignedCookieConfig {
	return web.SignedCookieConfig{
		Name:   "session",
		Key:    testSigningKey,
		Secure: false,
	}
}

func TestSignedCookieRoundTrip(t *testing.T) {
	cfg := testSignedCookieConfig()

	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSignedCookie(w, cfg, "hello"))

	r := &http.Request{Header: http.Header{"Cookie": w.Result().Header["Set-Cookie"]}}
	got, err := web.ReadSignedCookie(r, cfg)
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestSignedCookieNotFound(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := web.ReadSignedCookie(r, testSignedCookieConfig())
	assert.ErrorIs(t, err, web.ErrSignedCookieNotFound)
}

func TestSignedCookieTamperedValue(t *testing.T) {
	cfg := testSignedCookieConfig()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cfg.Name, Value: "aGVsbG8.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	_, err := web.ReadSignedCookie(r, cfg)
	assert.ErrorIs(t, err, web.ErrSignedCookieInvalid)
}

func TestSignedCookieWrongKey(t *testing.T) {
	cfg := testSignedCookieConfig()

	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSignedCookie(w, cfg, "hello"))

	wrongKey := cfg
	wrongKey.Key = []byte("totally-different-key-32-bytes!!!")
	r := &http.Request{Header: http.Header{"Cookie": w.Result().Header["Set-Cookie"]}}
	_, err := web.ReadSignedCookie(r, wrongKey)
	assert.ErrorIs(t, err, web.ErrSignedCookieInvalid)
}

func TestDeleteSignedCookie(t *testing.T) {
	cfg := testSignedCookieConfig()

	w := httptest.NewRecorder()
	web.DeleteSignedCookie(w, cfg)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, cfg.Name, cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestSignedCookieDefaults(t *testing.T) {
	cfg := testSignedCookieConfig()

	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSignedCookie(w, cfg, "v"))

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.True(t, c.HttpOnly)
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
}

func TestSignedCookieValueWithDot(t *testing.T) {
	cfg := testSignedCookieConfig()

	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSignedCookie(w, cfg, "a.b.c"))

	r := &http.Request{Header: http.Header{"Cookie": w.Result().Header["Set-Cookie"]}}
	got, err := web.ReadSignedCookie(r, cfg)
	require.NoError(t, err)
	assert.Equal(t, "a.b.c", got)
}

func TestSignedCookieWeakKey(t *testing.T) {
	cfg := testSignedCookieConfig()
	cfg.Key = []byte("too-short")

	w := httptest.NewRecorder()
	err := web.WriteSignedCookie(w, cfg, "hello")
	assert.ErrorIs(t, err, web.ErrWeakKey)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cfg.Name, Value: "hello.sig"})
	_, err = web.ReadSignedCookie(r, cfg)
	assert.ErrorIs(t, err, web.ErrWeakKey)
}

func TestWriteReadSession(t *testing.T) {
	cfg := testSignedCookieConfig()
	session := &web.Session{ID: "user-abc"}

	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSession(w, cfg, session))

	r := &http.Request{Header: http.Header{"Cookie": w.Result().Header["Set-Cookie"]}}
	got, err := web.ReadSignedCookie(r, cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, got)
}

func TestSessionValid(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		s := &web.Session{ID: "x"}
		assert.True(t, s.Valid())
	})
	t.Run("empty id", func(t *testing.T) {
		s := &web.Session{}
		assert.False(t, s.Valid())
	})
	t.Run("nil", func(t *testing.T) {
		var s *web.Session
		assert.False(t, s.Valid())
	})
	t.Run("expired", func(t *testing.T) {
		s := &web.Session{ID: "x", ExpiresAt: time.Now().Add(-time.Minute)}
		assert.False(t, s.Valid())
	})
	t.Run("not yet expired", func(t *testing.T) {
		s := &web.Session{ID: "x", ExpiresAt: time.Now().Add(time.Hour)}
		assert.True(t, s.Valid())
	})
}

func TestSessionMiddleware(t *testing.T) {
	cfg := testSignedCookieConfig()

	session := &web.Session{ID: "mid-user"}
	w := httptest.NewRecorder()
	require.NoError(t, web.WriteSession(w, cfg, session))
	cookieHeader := w.Result().Header["Set-Cookie"]

	t.Run("injects valid session", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header["Cookie"] = cookieHeader

		var got *web.Session
		handler := web.SessionMiddleware(web.WithSignedCookie(cfg))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got, _ = web.SessionFromContext(r.Context())
		}))
		handler.ServeHTTP(httptest.NewRecorder(), r)

		require.NotNil(t, got)
		assert.Equal(t, "mid-user", got.ID)
	})

	t.Run("no cookie passes through without session", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		var called bool
		handler := web.SessionMiddleware(web.WithSignedCookie(cfg))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			s, ok := web.SessionFromContext(r.Context())
			assert.False(t, ok)
			assert.Nil(t, s)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), r)
		assert.True(t, called)
	})

	t.Run("expired session not injected", func(t *testing.T) {
		expired := &web.Session{ID: "old", ExpiresAt: time.Now().Add(-time.Minute)}
		ew := httptest.NewRecorder()
		require.NoError(t, web.WriteSession(ew, cfg, expired))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header["Cookie"] = ew.Result().Header["Set-Cookie"]

		handler := web.SessionMiddleware(web.WithSignedCookie(cfg))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			s, ok := web.SessionFromContext(r.Context())
			assert.False(t, ok)
			assert.Nil(t, s)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), r)
	})
}
