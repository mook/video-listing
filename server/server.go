// Package server is responsible for the HTML UI
package server

import (
	_ "embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/mook/video-listing/injest"
	"github.com/sirupsen/logrus"
)

//go:embed listing.html
var templateText string
var tmpl = template.Must(template.New("listing.html").Parse(templateText))

// server is the main structure for the server; individual paths are in their
// own files.
type server struct {
	root        string
	colorRegexp *regexp.Regexp
	// A function taking a path relative to the root, which queues it to be injested.
	queue injest.Queue
}

func NewServer(root string, queue injest.Queue) http.Handler {
	s := &server{
		root:        root,
		colorRegexp: regexp.MustCompile(`^[0-9a-f]{3}$`),
		queue:       queue,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /l/", http.StripPrefix("/l", http.HandlerFunc(s.ServeListing)))
	mux.Handle("GET /j/", http.StripPrefix("/j", http.HandlerFunc(s.ServeJSON)))
	mux.Handle("POST /m/", http.StripPrefix("/m", http.HandlerFunc(s.ServeMark)))
	mux.Handle("POST /o/", http.StripPrefix("/o", http.HandlerFunc(s.ServeOverride)))
	mux.Handle("GET /i/folder.svg", http.HandlerFunc(s.ServeFallbackImage))
	mux.Handle("GET /i/mediaFolder.svg", http.HandlerFunc(s.ServeFallbackImage))
	mux.Handle("GET /i/video.svg", http.HandlerFunc(s.ServeFallbackImage))
	mux.Handle("GET /i/", http.StripPrefix("/i", http.HandlerFunc(s.ServeImage)))
	mux.Handle("GET /{$}", http.RedirectHandler("/l/", http.StatusFound))

	return mux
}

// getPath parses the path out of a HTTP request, returning the path to the
// corresponding file or directory on disk.  It also returns whether the given
// path is a directory.
func (s *server) getPath(w http.ResponseWriter, req *http.Request) (string, bool, error) {
	relPath := path.Clean(strings.Trim(req.URL.Path, "/"))
	if !fs.ValidPath(relPath) {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, relPath)
		logrus.WithError(err).WithField("path", relPath).Debug("Invalid client request path")
		return "", false, fmt.Errorf("Invalid client request path")
	}

	fullPath := path.Join(s.root, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		logrus.WithError(err).WithField("path", fullPath).Debug("Failed to stat file")
		_, _ = fmt.Fprintf(w, `Failed to check path "%s"`, relPath)
		return "", false, err
	}

	if !info.IsDir() && !info.Mode().IsRegular() {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, relPath)
		logrus.WithError(err).WithField("path", fullPath).Debug("Not a regular file")
		return "", false, fmt.Errorf("%s is not a directory or a regular file", fullPath)
	}

	return fullPath, info.IsDir(), nil
}
