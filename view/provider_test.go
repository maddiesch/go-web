package view_test

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maddiesch/go-web/view"
)

//go:embed all:example
var exampleFS embed.FS

func TestProviderRender(t *testing.T) {
	sub, err := fs.Sub(exampleFS, "example")
	require.NoError(t, err)

	p := view.NewProvider(sub)

	data := struct{ Greeting string }{
		Greeting: "Hello, World!",
	}

	var buf bytes.Buffer
	err = p.Render(&buf, "landing.html", data)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "<h1>Landing Page</h1>")
	assert.Contains(t, out, "<!DOCTYPE html>")
	assert.Contains(t, out, "<p>Hello, World!</p>")

	var buf2 bytes.Buffer
	err = p.Render(&buf2, "landing.html", nil)
	require.NoError(t, err)

	out2 := buf2.String()
	assert.Contains(t, out2, "<h1>Landing Page</h1>")
	assert.Contains(t, out2, "<!DOCTYPE html>")
	assert.NotContains(t, out2, "<p>")
}

func TestRenderError(t *testing.T) {
	sub, err := fs.Sub(exampleFS, "example")
	require.NoError(t, err)

	p := view.NewProvider(sub)

	t.Run("uses status-specific template when it exists", func(t *testing.T) {
		w := httptest.NewRecorder()
		p.RenderError(w, http.StatusNotFound, errors.New("page not found"), nil)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "404 Error Page")
		assert.Contains(t, w.Body.String(), "page not found")
	})

	t.Run("falls back to default template when status-specific is missing", func(t *testing.T) {
		w := httptest.NewRecorder()
		p.RenderError(w, http.StatusInternalServerError, errors.New("something exploded"), nil)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Error Page")
		assert.Contains(t, w.Body.String(), "something exploded")
	})

	t.Run("populates RenderErrorData fields", func(t *testing.T) {
		w := httptest.NewRecorder()
		p.RenderError(w, http.StatusNotFound, errors.New("missing resource"), nil)

		body := w.Body.String()
		assert.Contains(t, body, "404")
		assert.Contains(t, body, "Not Found")
		assert.Contains(t, body, "missing resource")
	})

	t.Run("sets correct status code in response", func(t *testing.T) {
		for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusInternalServerError} {
			w := httptest.NewRecorder()
			p.RenderError(w, status, errors.New("err"), nil)
			assert.Equal(t, status, w.Code)
		}
	})

	t.Run("passes user data through", func(t *testing.T) {
		type userData struct{ RequestID string }

		w := httptest.NewRecorder()
		p.RenderError(w, http.StatusNotFound, errors.New("not found"), userData{RequestID: "abc-123"})

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestProviderFuncMap(t *testing.T) {
	sub, err := fs.Sub(exampleFS, "example")
	require.NoError(t, err)

	p := view.NewProvider(sub)

	data := struct {
		Name      string
		Tags      []string
		Username  string
		Active    bool
		Count     int
		CreatedAt time.Time
	}{
		Name:      "Hello, World!",
		Tags:      []string{"go", "web", "templates"},
		Username:  "",
		Active:    true,
		Count:     7,
		CreatedAt: time.Now().Add(-3 * time.Hour),
	}

	var buf bytes.Buffer
	err = p.Render(&buf, "funcs.html", data)
	require.NoError(t, err)

	out := buf.String()

	// String helpers
	assert.Contains(t, out, "HELLO, WORLD!")
	assert.Contains(t, out, "hello, world!")
	assert.Contains(t, out, "Hello, W…")
	assert.Contains(t, out, "go, web, templates")
	assert.Contains(t, out, ">yes<")
	assert.Contains(t, out, "Hello, Go!")

	// Logic helpers
	assert.Contains(t, out, "anonymous") // default with empty username
	assert.Contains(t, out, ">active<")  // ternary with Active=true

	// Math helpers
	assert.Contains(t, out, ">17<") // add 7+10
	assert.Contains(t, out, ">4<")  // sub 7-3
	assert.Contains(t, out, ">3<")  // mod 7%4

	// Time helpers
	assert.Contains(t, out, "3h ago")
	assert.Contains(t, out, time.Now().Format("2006-01-02"))
}
