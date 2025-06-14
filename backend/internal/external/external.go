package external

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func FrontendRevProxy(targetURL string) http.Handler {
	target, err := url.Parse(targetURL)
	if err != nil {
		slog.Error(err.Error())
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		req.Host = target.Host
		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
	}

	return proxy
}

func SPAHandler(staticDir string) http.HandlerFunc {
	fs := http.FileServer(http.Dir(staticDir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(staticDir, r.URL.Path)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	}
}

func StaticPageHandler(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := strings.TrimPrefix(r.URL.Path, "/stat/")
		pagePath := filepath.Join(staticDir, page)

		if _, err := os.Stat(pagePath); err == nil {
			http.ServeFile(w, r, pagePath)
			return
		}
		if !strings.Contains(filepath.Base(page), ".") {
			pagePathHTML := pagePath + ".html"
			if _, err := os.Stat(pagePathHTML); err == nil {
				http.ServeFile(w, r, pagePathHTML)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Test page not found: %s", page)
	}
}
