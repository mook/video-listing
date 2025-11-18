package server

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mook/video-listing/injest"
	"github.com/sirupsen/logrus"
)

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
	// The full path from the root, URL escaped.
	EscapedFullPath string
	HasMedia        bool
	Native          string
	English         string
	// Whether this directory has been completely seen.
	Seen bool
}

type fileInput struct {
	// The base name of the file.
	Name            string
	EscapedFullPath string
	// The short title of the file.
	Title string
	// Whether this file has been seen.
	Seen bool
}

type templateInput struct {
	// The base name of the current directory.
	Name    string
	Native  string
	English string
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

	info, err := injest.ReadInfo(fullPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logrus.WithError(err).WithField("path", fullPath).Error("Error reading directory")
		_, _ = fmt.Fprintf(w, `Failed to list directory "%s"`, req.URL.Path)
		return
	}

	input := templateInput{
		Name:    path.Base(fullPath),
		Native:  info.NativeTitle,
		English: info.EnglishTitle,
		Path:    strings.Trim(req.URL.Path, "/"),
	}

	var escapedPathParts []string
	for p := range strings.SplitSeq(input.Path, "/") {
		if p != "" {
			escapedPathParts = append(escapedPathParts, url.PathEscape(p))
		}
	}

	for directory := range info.Injested {
		child := directoryInput{
			Name:            directory,
			EscapedFullPath: strings.Join(append(slices.Clone(escapedPathParts), url.PathEscape(directory)), "/"),
		}
		childInfo, err := injest.ReadInfo(filepath.Join(fullPath, directory))
		if err == nil {
			child.HasMedia = len(childInfo.Seen) > 0
			child.Native = childInfo.NativeTitle
			child.English = childInfo.EnglishTitle
			child.Seen = true
			for _, childSeen := range childInfo.Seen {
				child.Seen = child.Seen && childSeen
			}
		}
		input.Directories = append(input.Directories, child)
	}
	slices.SortFunc(input.Directories, func(a, b directoryInput) int {
		return cmp.Compare(a.Name, b.Name)
	})

	for file, seen := range info.Seen {
		input.Files = append(input.Files, fileInput{
			Name:            file,
			EscapedFullPath: path.Join(append(slices.Clone(escapedPathParts), file)...),
			Title:           file,
			Seen:            seen,
		})
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
	slices.SortFunc(input.Files, func(a, b fileInput) int {
		return cmp.Compare(a.Title, b.Title)
	})

	if strings.Trim(req.URL.Path, "/") == "" {
		input.Path = "" // Avoid double slash in URL
	}

	err = tmpl.Execute(w, input)
	if err != nil {
		logrus.WithError(err).Error("Failed to render template")
	}
	logrus.Debugf("Template rendered: %+v", input)
}
