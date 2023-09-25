package listing

import (
	"net/http"
)

type CorsHandler struct {
	http.Handler
}

func (h *CorsHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "GET, PUT")
	w.Header().Add("Vary", "Origin")

	if req.Method != "OPTIONS" {
		h.Handler.ServeHTTP(w, req)
	}
}
