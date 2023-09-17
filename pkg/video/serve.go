package video

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/mook/video-listing/pkg/transcoder"
	"github.com/sirupsen/logrus"
)

type VideoHandler struct {
}

func (h *VideoHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	urlPath := strings.ToLower(strings.Trim(req.URL.Path, "/"))
	playlistPath := path.Join("/cache", urlPath, transcoder.PlaylistName)
	_, err := os.Stat(playlistPath)
	if err == nil {
		http.ServeFile(w, req, playlistPath)
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		logrus.WithError(err).WithField("path", urlPath).Error("Error getting existing playlist")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Error reading playlist: %s", err))
		return
	}
	result, err := transcoder.Transcode(urlPath, "TODO")
	if err != nil {
		logrus.WithError(err).WithField("path", urlPath).Error("Error transcoding")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Error transcoding %s: %s", urlPath, err))
	} else {
		http.ServeFile(w, req, result)
	}
}
