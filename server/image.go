package server

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

const fallbackIconImage = `
	<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" fill="none"
	viewBox="0 0 24 24">
		<path fill="#%[1]s" d="M10.054 3 8.387 8h5.892l1.667-5z"/>
		<path fill="#%[1]s" d="
			M7.946 3 6.279 8H2v2h20V8h-5.613l1.667-5H20.6A2.4 2.4 0 0 1 23
			5.4v13.2a2.4 2.4 0 0 1-2.4 2.4H3.4A2.4 2.4 0 0 1 1 18.6V5.4A2.4
			2.4 0 0 1 3.4 3z"/>
	</svg>
`

func (s *server) ServeFallbackIcon(w http.ResponseWriter, req *http.Request) {
	color := "666"
	if s.colorRegexp.MatchString(req.URL.RawQuery) {
		color = req.URL.RawQuery
	}
	w.Header().Add("Content-Type", "image/svg+xml")
	w.WriteHeader(http.StatusOK)
	// TODO: ETag / If-None-Match handling
	fmt.Fprintf(w, fallbackIconImage, color)
}

func (s *server) ServeImage(w http.ResponseWriter, req *http.Request) {
	directory, err := s.getPath(w, req, true)
	if err != nil {
		return // Already wrote the response
	}
	log := logrus.WithField("path", directory)
	f, err := os.Open(filepath.Join(directory, ".cover.jpg"))
	log.WithError(err).Debug("Opened cover image")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	if err != nil {
		log.WithError(err).Debug("Failed to write cover image")
	}
}
