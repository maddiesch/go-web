package view

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Provider struct {
	ViewFS  fs.FS
	Include []string
	Layout  string
	Func    template.FuncMap

	cacheMu sync.RWMutex
	cache   map[string]*template.Template
}

func NewProvider(viewFS fs.FS) *Provider {
	funcMap := template.FuncMap{
		// String helpers
		"upper":     strings.ToUpper,
		"lower":     strings.ToLower,
		"title":     strings.ToTitle,
		"trim":      strings.TrimSpace,
		"replace":   strings.ReplaceAll,
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"join":      strings.Join,
		"split":     strings.Split,
		"truncate": func(n int, s string) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},

		// Safe HTML/URL passthrough
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"safeAttr": func(s string) template.HTMLAttr { return template.HTMLAttr(s) },
		"safeURL":  func(s string) template.URL { return template.URL(s) },

		// Time helpers
		"now":        time.Now,
		"formatTime": func(layout string, t time.Time) string { return t.Format(layout) },
		"timeAgo": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			case d < 24*time.Hour:
				return fmt.Sprintf("%dh ago", int(d.Hours()))
			case d < 7*24*time.Hour:
				return fmt.Sprintf("%dd ago", int(d.Hours()/24))
			default:
				return t.Format("Jan 2, 2006")
			}
		},

		// Logic helpers
		"default": func(def, val any) any {
			if val == nil || val == "" || val == 0 || val == false {
				return def
			}
			return val
		},
		"ternary": func(t, f any, cond bool) any {
			if cond {
				return t
			}
			return f
		},

		// Numeric helpers
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"mod": func(a, b int) int { return a % b },

		// Collection helpers
		"first": func(s []any) any {
			if len(s) == 0 {
				return nil
			}
			return s[0]
		},
		"last": func(s []any) any {
			if len(s) == 0 {
				return nil
			}
			return s[len(s)-1]
		},
		"len": func(s any) int {
			switch v := s.(type) {
			case string:
				return len(v)
			case []any:
				return len(v)
			case map[string]any:
				return len(v)
			default:
				return 0
			}
		},
	}

	return &Provider{
		ViewFS:  viewFS,
		Include: []string{"include/*.html.template"},
		Layout:  "_layout.html.template",
		Func:    funcMap,
		cache:   make(map[string]*template.Template),
	}
}

func (p *Provider) Render(w io.Writer, name string, data any) error {
	slog.Debug("view.Provider Render", slog.String("template", name))

	p.cacheMu.RLock()
	tmpl, ok := p.cache[name]
	p.cacheMu.RUnlock()

	if !ok {
		paths := []string{p.Layout}
		for _, include := range p.Include {
			glob, _ := fs.Glob(p.ViewFS, include)
			if len(glob) > 0 {
				paths = append(paths, glob...)
			}
		}
		paths = append(paths, name+".template")

		var err error
		tmpl, err = template.New(p.Layout).Funcs(p.Func).ParseFS(p.ViewFS, paths...)
		if err != nil {
			return err
		}

		p.cacheMu.Lock()
		p.cache[name] = tmpl
		p.cacheMu.Unlock()
	}

	return tmpl.ExecuteTemplate(w, p.Layout, data)
}
