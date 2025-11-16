package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/sirupsen/logrus"
)

func (s *server) ServeMark(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fullPath, err := s.getPath(w, req, false)
	if err != nil {
		// Already emitted the error to the client
		return
	}

	state, err := strconv.ParseBool(req.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, `Invalid new state %q`, req.URL.RawQuery)
		logrus.WithError(err).Debug("Invalid client request query")
		return
	}

	dir, base := path.Split(fullPath)
	base = fmt.Sprintf(".%s.seen", base)
	fullPath = path.Join(dir, base)
	if state {
		if f, err := os.Create(fullPath); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			logrus.WithError(err).WithField("path", fullPath).Error("Failed to create marker file")
			_, _ = fmt.Fprintf(w, `Failed to create marker for "%s"`, req.URL.Path)
			return
		} else {
			_ = f.Close()
			logrus.WithField("path", fullPath).Debug("File marked as seen")
		}
	} else {
		if err := os.Remove(fullPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			w.WriteHeader(http.StatusInternalServerError)
			logrus.WithError(err).WithField("path", fullPath).Error("Failed to remove marker file")
			_, _ = fmt.Fprintf(w, `Failed to remove marker for "%s"`, req.URL.Path)
			return
		}
		logrus.WithField("path", fullPath).Debug("File marked as not seen")
	}
}
