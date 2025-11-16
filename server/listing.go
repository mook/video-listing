package server

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/sirupsen/logrus"
)

func (s *server) ServeListing(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fullPath, err := s.getPath(w, req, true)
	if err != nil {
		// Already emitted the error to the client
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).WithField("path", fullPath).Error("Error reading directory")
		_, _ = fmt.Fprintf(w, `Failed to list directory "%s"`, req.URL.Path)
		return
	}

	var directories, files []directoryEntry

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			_, err = os.Stat(path.Join(fullPath, entry.Name(), ".seen"))
			directories = append(directories, directoryEntry{
				Name: entry.Name(),
				Seen: err == nil,
			})
		} else if entry.Type().IsRegular() {
			_, err = os.Stat(path.Join(fullPath, fmt.Sprintf(".%s.seen", entry.Name())))
			files = append(files, directoryEntry{
				Name: entry.Name(),
				Seen: err == nil,
			})
		}
	}

	data := map[string]any{
		"Name":        path.Base(fullPath),
		"Path":        "/" + strings.Trim(req.URL.Path, "/"),
		"Directories": directories,
		"Files":       files,
	}
	if strings.Trim(req.URL.Path, "/") == "" {
		data["Path"] = "" // Avoid double slash in URL
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		logrus.WithError(err).Error("Failed to render template")
	}
	logrus.WithFields(data).Debug("Template rendered")
}
