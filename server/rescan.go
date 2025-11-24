package server

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (s *server) ServeRescan(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fullPath, isDir, err := s.getPath(w, req)
	if err != nil {
		// Already emitted the error to the client
		return
	}
	if !isDir {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, req.URL.Path)
		logrus.WithError(err).WithField("path", fullPath).Debug("Not a directory")
		return
	}

	relPath, err := filepath.Rel(s.root, fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).WithField("path", fullPath).Error("Failed to get relative path")
		return
	}
	s.queue(relPath)
	w.WriteHeader(http.StatusAccepted)
}
