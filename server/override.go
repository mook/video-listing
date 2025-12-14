package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"

	"github.com/mook/video-listing/injest"
	"github.com/sirupsen/logrus"
)

func (s *server) ServeOverride(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fullPath, isDir, err := s.getPath(w, req)
	if err != nil {
		// Alreayd emitted the error to the client
		return
	}

	if !isDir {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid path "%s"`, req.URL.Path)
		logrus.WithError(err).WithField("path", fullPath).Debug("Not a directory")
		return
	}

	if req.Body == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `No request body`)
		logrus.WithError(err).WithField("path", fullPath).Debug("No request body")
	}
	defer req.Body.Close()

	var body struct {
		ID    int  `json:"id"`
		Force bool `json:"force"`
		Mark  bool `json:"mark"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "Failed to decode request body")
		logrus.WithError(err).WithField("path", fullPath).Error("Failed to decode request body")
		return
	}

	relPath, err := filepath.Rel(s.root, fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).WithField("path", fullPath).Error("Failed to get relative path")
		return
	}

	logrus.WithField("input", body).Debug("Processing override")
	var info *injest.InfoType
	if body.Mark {
		info, err = injest.ReadInfo(fullPath, true)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "Failed to read existing ID")
			logrus.WithError(err).WithField("path", relPath).Error("Failed to read existing ID")
			return
		}
		hasTrue := false
		hasFalse := false
		for v := range maps.Values(info.Seen) {
			if v {
				hasTrue = true
			} else {
				hasFalse = true
			}
			if hasTrue && hasFalse {
				break
			}
		}
		if !hasTrue || !hasFalse {
			if !hasTrue {
				for k := range info.Seen {
					info.Seen[k] = true
				}
			} else if !hasFalse {
				for k := range info.Seen {
					info.Seen[k] = false
				}
			}
			if err := injest.WriteInfo(fullPath, info); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, "Failed to update seen state")
				logrus.WithError(err).WithField("path", relPath).Error("Failed to update seen state")
				return
			}
		}
	}

	var existingID int
	if body.ID != 0 {
		if info == nil {
			info, err = injest.ReadInfo(fullPath, false)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, "Failed to read existing ID")
				logrus.WithError(err).WithField("path", relPath).Error("Failed to read existing ID")
				return
			}
		}
		existingID = info.AniListID
	}

	if body.ID != existingID || body.Force {
		s.queue(injest.QueueOptions{
			Directory: relPath,
			ID:        body.ID,
			Force:     body.Force,
		})
	}
	w.WriteHeader(http.StatusAccepted)
}
