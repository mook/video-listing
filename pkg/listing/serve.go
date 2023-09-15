package listing

import (
	_ "embed"
	"errors"
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

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/mook/video-listing/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// The length of a path part in the URL, in characters
	tokenLength = crc64.Size * 2
)

var (
	hashTable = crc64.MakeTable(crc64.ECMA)
)

// A directoryEntry holds information needed to render a page
type directoryEntry struct {
	Name        string        // The name of this directory
	Directories []fs.DirEntry // Any subdirectories
	Files       []fs.DirEntry // Any files in this directory
}

func hashName(name string) string {
	hash := crc64.Checksum([]byte(name), hashTable)
	return fmt.Sprintf("%0*x", tokenLength, hash)
}

type ListingHandler struct {
	template *template.Template

	// Cache for directory listings.  Note that the hash key is the raw path,
	// so there may be multiple entries for the same directory.
	cache *lru.Cache[string, *directoryEntry]
}

func NewListingHandler(resources fs.FS) (http.Handler, error) {
	var err error
	tmpl := template.New("listing.html").Funcs(template.FuncMap{
		"hashName": hashName,
	})
	if tmpl, err = tmpl.ParseFS(resources, "listing.html"); err != nil {
		return nil, fmt.Errorf("failed to parse listing template: %w", err)
	}
	cache, err := lru.New[string, *directoryEntry](64)
	if err != nil {
		logrus.Debugf("Failed to create cache, going without: %v", err)
		cache = nil
	}
	return &ListingHandler{
		template: tmpl,
		cache:    cache,
	}, nil
}

func (h *ListingHandler) readDirectory(urlPath string) (*directoryEntry, error) {
	dir, err := os.OpenFile("/media", os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open media root: %w", err)
	}
	for _, part := range strings.Split(urlPath, "/") {
		if part == "" {
			continue
		}
		if len(part) != tokenLength {
			return nil, fmt.Errorf("invalid path %s", part)
		}
		entries, err := dir.ReadDir(0)
		if err != nil {
			return nil, fmt.Errorf("failed to enumerate %s: %w", dir.Name(), err)
		}
		found := false
		for _, entry := range entries {
			if hashName(entry.Name()) == part {
				found = true
				name := path.Join(dir.Name(), entry.Name())
				fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_DIRECTORY|unix.O_NOATIME, 0)
				if err != nil {
					return nil, fmt.Errorf("failed to open directory %s: %w", name, err)
				}
				dir = os.NewFile(uintptr(fd), name)
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("could not find item %s in %s: %w", part, dir.Name(), fs.ErrNotExist)
		}
	}

	children, err := dir.ReadDir(0)
	if err != nil {
		return nil, fmt.Errorf("could not list child entries: %w", err)
	}

	slices.SortFunc(children, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	return &directoryEntry{
		Name: dir.Name(),
		Directories: utils.Filter(children, func(e fs.DirEntry) bool {
			return e.IsDir()
		}),
		Files: utils.Filter(children, func(e fs.DirEntry) bool {
			return !e.IsDir()
		}),
	}, nil
}

func (h *ListingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var err error
	urlPath := strings.ToLower(strings.Trim(req.URL.Path, "/"))
	entry, _ := h.cache.Get(urlPath)
	if entry == nil {
		entry, err = h.readDirectory(urlPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			io.WriteString(w, err.Error())
			return
		}
		h.cache.Add(urlPath, entry)
	}

	err = h.template.Execute(w, entry)
	if err != nil {
		fmt.Printf("Failed to render template for %s: %s\n", entry.Name, err)
	}
}
