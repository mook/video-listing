package server

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

var fallbackImages = map[string]string{
	"folder.svg": `
		<svg xmlns="http://www.w3.org/2000/svg" height="24px" width="24px"
			viewBox="0 -960 960 960" fill="#%[1]s">
			<path d="M160-160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800h240l80
				80h320q33 0 56.5 23.5T880-640v400q0 33-23.5
				56.5T800-160H160Zm0-80h640v-400H447l-80-80H160v480Zm0
				0v-480 480Z"/>
		</svg>`,
	"mediaFolder.svg": `
		<svg xmlns="http://www.w3.org/2000/svg" height="24px" width="24px"
			viewBox="0 -960 960 960" fill="#%[1]s">
			<path d="M360-440h400L622-620l-92 120-62-80-108 140ZM120-120q-33
				0-56.5-23.5T40-200v-520h80v520h680v80H120Zm160-160q-33
				0-56.5-23.5T200-360v-440q0-33 23.5-56.5T280-880h200l80
				80h280q33 0 56.5 23.5T920-720v360q0 33-23.5
				56.5T840-280H280Zm0-80h560v-360H527l-80-80H280v440Zm0 0v-440 440Z"/>
		</svg>`,
	"video.svg": `
		<svg xmlns="http://www.w3.org/2000/svg" height="24px" width="24px"
			viewBox="0 -960 960 960" fill="#%[1]s">
			<path d="m160-800 80 160h120l-80-160h80l80 160h120l-80-160h80l80
				160h120l-80-160h120q33 0 56.5 23.5T880-720v480q0 33-23.5
				56.5T800-160H160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800Zm0
				240v320h640v-320H160Zm0 0v320-320Z"/>
		</svg>`,
}

func (s *server) ServeFallbackImage(w http.ResponseWriter, req *http.Request) {
	color := "666"
	if s.colorRegexp.MatchString(req.URL.RawQuery) {
		color = req.URL.RawQuery
	}
	baseName := path.Base(req.URL.Path)
	if value, ok := fallbackImages[baseName]; ok {
		w.Header().Add("Content-Type", "image/svg+xml")
		w.WriteHeader(http.StatusOK)
		// TODO: ETag / If-None-Match handling
		fmt.Fprintf(w, value, color)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *server) ServeImage(w http.ResponseWriter, req *http.Request) {
	fullPath, isDir, err := s.getPath(w, req)
	if err != nil {
		return // Already wrote the response
	}
	log := logrus.WithField("path", fullPath)
	var f io.ReadCloser
	if isDir {
		f, err = os.Open(filepath.Join(fullPath, ".cover.jpg"))
		log.WithError(err).Debug("Opened cover image")
	} else {
		dir, base := filepath.Split(fullPath)
		name := fmt.Sprintf(".%s.jpg", base)
		f, err = os.Open(filepath.Join(dir, name))
		log.WithError(err).Debug("Opened thumbnail")
	}
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
