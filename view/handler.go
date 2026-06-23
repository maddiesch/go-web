package view

import (
	"bytes"
	"io"
	"net/http"
)

func TemplateHandler(provider *Provider, name string, dataFn func(*http.Request) any) http.Handler {
	return &templateHandler{
		provider: provider,
		name:     name,
		dataFn:   dataFn,
		BeforeRender: func(_ *http.Request, data any) (any, error) {
			return data, nil
		},
		BeforeWrite: func(w http.ResponseWriter, r *http.Request, data any) error {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)

			return nil
		},
	}
}

type templateHandler struct {
	provider     *Provider
	name         string
	dataFn       func(*http.Request) any
	BeforeRender func(*http.Request, any) (any, error)
	BeforeWrite  func(http.ResponseWriter, *http.Request, any) error
}

func (h *templateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	buffer := bytes.NewBuffer(nil)
	var data any
	if h.dataFn != nil {
		data = h.dataFn(r)
	}

	if h.BeforeRender != nil {
		var err error
		data, err = h.BeforeRender(r, data)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	if err := h.provider.Render(buffer, h.name, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if h.BeforeWrite != nil {
		if err := h.BeforeWrite(w, r, data); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	io.Copy(w, buffer)
}
