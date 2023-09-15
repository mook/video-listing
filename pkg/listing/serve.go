package listing

import (
	_ "embed"
	"fmt"
	"hash/crc64"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/mook/video-listing/pkg/utils"
	"golang.org/x/sys/unix"
)

var (
	hashTable = crc64.MakeTable(crc64.ECMA)
)

func hashName(name string) string {
	hash := crc64.Checksum([]byte(name), hashTable)
	return fmt.Sprintf("%0*x", crc64.Size*2, hash)
}

func internalError(w http.ResponseWriter, err error, format string, a ...any) {
	args := append(a, err)
	w.WriteHeader(http.StatusInternalServerError)
	io.WriteString(w, fmt.Sprintf(format+": %s", args...))
}

type ListingHandler struct {
	template *template.Template
}

func NewListingHandler(resources fs.FS) (http.Handler, error) {
	var err error
	tmpl := template.New("listing.html").Funcs(template.FuncMap{
		"hashName": hashName,
	})
	if tmpl, err = tmpl.ParseFS(resources, "listing.html"); err != nil {
		return nil, fmt.Errorf("failed to parse listing template: %w", err)
	}
	return &ListingHandler{
		template: tmpl,
	}, nil
}

func (h *ListingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	dir, err := os.OpenFile("/media", os.O_RDONLY, 0)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Failed to open media root: %s", err))
		return
	}
	for _, part := range strings.Split(req.URL.Path, "/") {
		if part == "" {
			continue
		}
		entries, err := dir.ReadDir(0)
		if err != nil {
			internalError(w, err, "Failed to enumerate %s", dir.Name())
			return
		}
		found := false
		for _, entry := range entries {
			if hashName(entry.Name()) == part {
				found = true
				name := path.Join(dir.Name(), entry.Name())
				fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_DIRECTORY|unix.O_NOATIME, 0)
				if err != nil {
					internalError(w, err, "Failed to open directory %s", name)
					return
				}
				dir = os.NewFile(uintptr(fd), name)
				break
			}
		}
		if !found {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, fmt.Sprintf("Could not find item %s in %s", part, dir.Name()))
			return
		}
	}

	children, err := dir.ReadDir(0)
	if err != nil {
		internalError(w, err, "could not list child entries")
		return
	}

	slices.SortFunc(children, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	err = h.template.Execute(w, map[string]interface{}{
		"Name": dir.Name(),
		"Directories": utils.Filter(children, func(e fs.DirEntry) bool {
			return e.IsDir()
		}),
		"Files": utils.Filter(children, func(e fs.DirEntry) bool {
			return !e.IsDir()
		}),
	})
	if err != nil {
		fmt.Printf("Failed to render template for %s: %s\n", dir.Name(), err)
	}
}
