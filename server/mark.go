package server

import (
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/mook/video-listing/injest"
	"github.com/sirupsen/logrus"
)

func (s *server) ServeMark(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fullPath, isDir, err := s.getPath(w, req)
	if err != nil {
		// Already emitted the error to the client
		return
	}

	if isDir {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, req.URL.Path)
		logrus.WithError(err).WithField("path", fullPath).Debug("Not a regular file")
		return
	}

	state, err := strconv.ParseBool(req.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logrus.WithError(err).Debug("Invalid client request query")
		_, _ = fmt.Fprintf(w, `Invalid new state %q`, req.URL.RawQuery)
		return
	}

	dir, base := path.Split(fullPath)
	info, err := injest.ReadInfo(dir, false)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).Debug("Error reading state")
		_, _ = fmt.Fprintf(w, `Error reading state`)
		return
	}
	if _, ok := info.Seen[base]; !ok {
		w.WriteHeader(http.StatusNotFound)
		logrus.WithError(err).Debug("Writing state for invalid file")
		return
	}

	info.Seen[base] = state

	if err := injest.WriteInfo(dir, info); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).Debug("Error writing state")
		_, _ = fmt.Fprintf(w, `Error writing state`)
		return
	}

	rootInfo, err := injest.ReadInfo(s.root, false)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).Debug("Error reading root state")
		_, _ = fmt.Fprintf(w, "Error reading root state")
		return
	}
	relPath, err := filepath.Rel(s.root, dir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).Debug("Error getting relative path")
		_, _ = fmt.Fprintf(w, "Error getting relative path")
	}
	const maxRecent = 5
	recents := append([]string{relPath}, rootInfo.Recents...)
	rootInfo.Recents = make([]string, 0, maxRecent)
	for _, item := range recents {
		if !slices.Contains(rootInfo.Recents, item) {
			rootInfo.Recents = append(rootInfo.Recents, item)
			if len(rootInfo.Recents) >= maxRecent {
				break
			}
		}
	}
	if err := injest.WriteInfo(s.root, rootInfo); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).Debug("Error writing root state")
	}
	_, _ = fmt.Fprintf(w, "Error writing root state")
}
