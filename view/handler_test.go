package view_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maddiesch/go-web/view"
)

func testProvider(t *testing.T) *view.Provider {
	t.Helper()

	sub, err := fs.Sub(exampleFS, "example")
	require.NoError(t, err)

	return view.NewProvider(sub)
}

func TestTemplateHandler(t *testing.T) {
	p := testProvider(t)

	t.Run("renders template with data", func(t *testing.T) {
		handler := view.TemplateHandler(p, "landing.html", func(r *http.Request) any {
			return struct{ Greeting string }{Greeting: "Hello, Test!"}
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), "<h1>Landing Page</h1>")
		assert.Contains(t, rec.Body.String(), "<p>Hello, Test!</p>")
	})

	t.Run("renders template with nil dataFn", func(t *testing.T) {
		handler := view.TemplateHandler(p, "landing.html", nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "<h1>Landing Page</h1>")
		assert.NotContains(t, rec.Body.String(), "<p>")
	})

	t.Run("returns 500 on unknown template", func(t *testing.T) {
		handler := view.TemplateHandler(p, "does-not-exist.html", nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
