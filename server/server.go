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
	"strings"

	"github.com/sirupsen/logrus"
)

//go:embed listing.html
var templateText string
var tmpl = template.Must(template.New("listing.html").Parse(templateText))

// server is the main structure for the server; individual paths are in their
// own files.
type server struct {
	root string
}

func NewServer(root string) http.Handler {
	s := &server{root: root}
	mux := http.NewServeMux()
	mux.Handle("/l/", http.StripPrefix("/l", http.HandlerFunc(s.ServeListing)))
	mux.Handle("POST /m/", http.StripPrefix("/m", http.HandlerFunc(s.ServeMark)))
	mux.Handle("/{$}", http.RedirectHandler("/l/", http.StatusFound))

	return mux
}

// getPath parses the path out of a HTTP request, returning the path to the
// corresponding file or directory on disk.  The function also checks whether
// the path is a directory, depending on the input.  If the check fails,
// the response will have been written to the response writer already.
func (s *server) getPath(w http.ResponseWriter, req *http.Request, isDir bool) (string, error) {
	relPath := path.Clean(strings.Trim(req.URL.Path, "/"))
	if !fs.ValidPath(relPath) {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, relPath)
		logrus.WithError(err).WithField("path", relPath).Debug("Invalid client request path")
		return "", fmt.Errorf("Invalid client request path")
	}

	fullPath := path.Join(s.root, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		logrus.WithError(err).WithField("path", fullPath).Debug("Failed to stat file")
		_, _ = fmt.Fprintf(w, `Failed to check path "%s"`, relPath)
		return "", err
	}

	if isDir {
		if !info.IsDir() {
			w.WriteHeader(http.StatusBadRequest)
			_, err := fmt.Fprintf(w, `Invalid path "%s"`, relPath)
			logrus.WithError(err).WithField("path", fullPath).Debug("Not a directory")
			return "", fmt.Errorf("%s is not a directory", fullPath)
		}
	} else if !info.Mode().IsRegular() {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, relPath)
		logrus.WithError(err).WithField("path", fullPath).Debug("Not a regular file")
		return "", fmt.Errorf("%s is not a regular file", fullPath)
	}

	return fullPath, nil
}

// directoryEntry describes one entry in the listing.
type directoryEntry struct {
	Name string
	Seen bool
}
