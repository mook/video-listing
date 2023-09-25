package listing

import (
	"net/http"
	"net/url"
)

type CorsHandler struct {
	http.Handler
}

func (h *CorsHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	origin, err := url.Parse(req.Header.Get("Origin"))
	if err != nil || origin.Hostname() != "mook.github.io" {
		h.Handler.ServeHTTP(w, req)
		return
	}

	w.Header().Add("Access-Control-Allow-Origin", "https://mook.github.io")
	w.Header().Add("Access-Control-Allow-Methods", "GET, PUT")
	w.Header().Add("Vary", "Origin")

	if req.Method != "OPTIONS" {
		h.Handler.ServeHTTP(w, req)
	}
}
