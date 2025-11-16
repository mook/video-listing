package server

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/sirupsen/logrus"
)

var excludedNames = map[string]struct{}{
	"Thumbs.db": {},
}

// commonLength returns the length of the longest common prefix or suffix for a
// slice of strings; note that the slice will be modified.
func commonLength(strings []string, isPrefix bool) int {
	// For our use, empty or single element strings should not have prefix or
	// suffix removed.
	if len(strings) < 2 {
		return 0
	}

	for offset := range len(strings[0]) {
		for i := range strings {
			if len(strings[i]) == offset {
				return offset
			}
			if isPrefix {
				if strings[i][offset] != strings[0][offset] {
					return offset
				}
			} else {
				if strings[i][len(strings[i])-1-offset] != strings[0][len(strings[0])-1-offset] {
					return offset
				}
			}
		}
	}

	// If all strings are equal, do not strip anything.  That should not be
	// possible anyway.
	return 0
}

type directoryInput struct {
	// The base name of the child directory.
	Name string
	// Whether this directory has been completely seen.
	Seen bool
}

type fileInput struct {
	// The base name of the file.
	Name string
	// The short title of the file.
	Title string
	// Whether this file has been seen.
	Seen bool
}

type templateInput struct {
	// The base name of the current directory.
	Name string
	// The full path to the current directory.
	Path        string
	Directories []directoryInput
	Files       []fileInput
}

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

	input := templateInput{
		Name: path.Base(fullPath),
		Path: "/" + strings.Trim(req.URL.Path, "/"),
	}

	for _, entry := range entries {
		if _, found := excludedNames[entry.Name()]; found {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			_, err = os.Stat(path.Join(fullPath, entry.Name(), ".seen"))
			input.Directories = append(input.Directories, directoryInput{
				Name: entry.Name(),
				Seen: err == nil,
			})
		} else if entry.Type().IsRegular() {
			_, err = os.Stat(path.Join(fullPath, fmt.Sprintf(".%s.seen", entry.Name())))
			input.Files = append(input.Files, fileInput{
				Name:  entry.Name(),
				Title: entry.Name(),
				Seen:  err == nil,
			})
		}
	}

	// Post process: Strip common prefix and suffix of the strings
	if len(input.Files) > 1 {
		titles := make([]string, 0, len(input.Files))
		for _, f := range input.Files {
			titles = append(titles, f.Name)
		}
		prefixLen := commonLength(titles, true)
		suffixLen := commonLength(titles, false)
		for i := range input.Files {
			input.Files[i].Title = input.Files[i].Title[prefixLen : len(input.Files[i].Title)-suffixLen]
		}
	}
	if strings.Trim(req.URL.Path, "/") == "" {
		input.Path = "" // Avoid double slash in URL
	}

	err = tmpl.Execute(w, input)
	if err != nil {
		logrus.WithError(err).Error("Failed to render template")
	}
	logrus.WithField("input", input).Debug("Template rendered")
}
