package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mook/video-listing/injest"
	"github.com/sirupsen/logrus"
)

func (s *server) ServeJSON(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
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

	info, err := injest.ReadInfo(fullPath, true)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).WithField("path", fullPath).Error("Error reading directory")
		_, _ = fmt.Fprintf(w, `Failed to list directory "%s"`, req.URL.Path)
		return
	}

	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(info); err != nil {
		logrus.WithError(err).WithField("path", fullPath).Error("Error emitting JSON")
	}
}
