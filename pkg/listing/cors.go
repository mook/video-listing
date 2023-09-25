package listing

import (
	"net/http"
	"net/url"

	"github.com/sirupsen/logrus"
)

type CorsHandler struct {
	http.Handler
}

func (h *CorsHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	origin, err := url.Parse(req.Header.Get("Origin"))
	logrus.WithError(err).Debugf("Serving request %s from %s to %s", req.URL, origin, req.RemoteAddr)
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "GET, PUT")
	w.Header().Add("Vary", "Origin")

	if req.Method != "OPTIONS" {
		h.Handler.ServeHTTP(w, req)
	}
}
