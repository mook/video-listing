package listing

import (
	"context"
	"database/sql"
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

// A directoryListing holds information needed to render a page
type directoryListing struct {
	Name        string   // The name of this directory
	Directories []string // Any subdirectories
	Files       []string // Any files in this directory
}

func hashName(name string) string {
	hash := crc64.Checksum([]byte(name), hashTable)
	return fmt.Sprintf("%0*x", tokenLength, hash)
}

type listingStatments struct {
	insert        *sql.Stmt // Insert a new entry
	queryName     *sql.Stmt // Query for one entry
	queryChildren *sql.Stmt // Query for entries
	access        *sql.Stmt // Update the last accessed time of an entry
}

type ListingHandler struct {
	template *template.Template
	dbConn   *sql.Conn
	stmts    listingStatments
}

func NewListingHandler(ctx context.Context, resources fs.FS, conn *sql.Conn) (http.Handler, error) {
	var err error
	var stmts listingStatments
	tmpl := template.New("listing.html").Funcs(template.FuncMap{
		"hashName": hashName,
	})
	if tmpl, err = tmpl.ParseFS(resources, "listing.html"); err != nil {
		return nil, fmt.Errorf("failed to parse listing template: %w", err)
	}
	if stmts, err = createDatabase(ctx, conn); err != nil {
		return nil, err
	}
	return &ListingHandler{
		template: tmpl,
		dbConn:   conn,
		stmts:    stmts,
	}, nil
}

func createDatabase(ctx context.Context, conn *sql.Conn) (listingStatments, error) {
	result := listingStatments{}
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS listing_cache (
			parent TEXT NOT NULL COLLATE NOCASE, -- URL path of parent
			hash TEXT NOT NULL COLLATE NOCASE,   -- Hash of this entry
			path TEXT NOT NULL COLLATE NOCASE,   -- Absolute file name
			is_dir INT CHECK (is_dir IN (0, 1)), -- Whether this entry is a directory
			last_used INT DEFAULT (unixepoch('now', 'utc')),
			PRIMARY KEY (parent, hash)
		) STRICT
	`)
	if err != nil {
		return result, fmt.Errorf("error creating table: %w", err)
	}
	_, err = conn.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_listing_cache ON listing_cache (parent)
	`)
	if err != nil {
		return result, fmt.Errorf("error creating index: %w", err)
	}
	result.insert, err = conn.PrepareContext(ctx, `
		INSERT OR REPLACE INTO listing_cache (parent, hash, path, is_dir) VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing insert: %w", err)
	}
	result.queryName, err = conn.PrepareContext(ctx, `
		SELECT path FROM listing_cache WHERE parent = ? AND hash = ? LIMIT 1
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing name query: %w", err)
	}
	result.queryChildren, err = conn.PrepareContext(ctx, `
		SELECT path, is_dir FROM listing_cache WHERE parent = ? AND hash != "" ORDER BY path ASC
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing children query: %w", err)
	}
	result.access, err = conn.PrepareContext(ctx, `
		UPDATE listing_cache SET last_used = unixepoch("now", "utc")
		WHERE parent = ?
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing access: %w", err)
	}
	_, _ = result.insert.Exec("", "", "/media", 1)
	return result, nil
}

// getListing returns a directory listing.  The parent is the clean URL path,
// all in lower case hash chunks joined by slash ("/") without any preceding or
// trailing slashes.
func (h *ListingHandler) getListing(ctx context.Context, parent string) (*directoryListing, error) {
	result := &directoryListing{}

	lastSlash := strings.LastIndex(parent, "/")
	grandParent := ""
	leaf := parent
	if lastSlash >= 0 {
		grandParent = parent[:lastSlash]
		leaf = parent[lastSlash+1:]
	}
	row := h.stmts.queryName.QueryRowContext(ctx, grandParent, leaf)
	if err := row.Scan(&result.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// This is just a cache miss (probably)
			return h.readDirectory(ctx, parent)
		}
		return nil, fmt.Errorf("failed to find directory %s: %w", parent, err)
	}

	rows, err := h.stmts.queryChildren.QueryContext(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("failed to query entries for %s: %w", parent, err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var name string
		var isDir bool
		if err = rows.Scan(&name, &isDir); err != nil {
			return nil, fmt.Errorf("error reading row: %w", err)
		}
		name = path.Base(name)
		if isDir {
			result.Directories = append(result.Directories, name)
		} else {
			result.Files = append(result.Files, name)
		}
		found = true
	}

	if !found {
		// If we found no children, this might be a cache miss instead.
		// Since the directory is empty, this should be cheap.
		return h.readDirectory(ctx, parent)
	}

	return result, nil
}

func (h *ListingHandler) readDirectory(ctx context.Context, urlPath string) (*directoryListing, error) {
	dir, err := os.OpenFile("/media", os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open media root: %w", err)
	}
	defer dir.Close()
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
				dir.Close()
				dir = os.NewFile(uintptr(fd), name)
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("could not find item %s in %s: %w", part, dir.Name(), fs.ErrNotExist)
		}
	}

	logrus.Debugf("Reading directory %s (%s)...", dir.Name(), urlPath)

	children, err := dir.ReadDir(0)
	if err != nil {
		return nil, fmt.Errorf("could not list child entries: %w", err)
	}

	slices.SortFunc(children, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	result := &directoryListing{
		Name: dir.Name(),
	}
	for _, entry := range children {
		// Insert into the cache, ignoring errors
		_, _ = h.stmts.insert.ExecContext(ctx,
			urlPath,
			hashName(entry.Name()),
			path.Join(dir.Name(), entry.Name()),
			entry.IsDir())
		if entry.IsDir() {
			result.Directories = append(result.Directories, entry.Name())
		} else {
			result.Files = append(result.Files, entry.Name())
		}
	}

	return result, nil
}

func (h *ListingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var err error
	urlPath := strings.ToLower(strings.Trim(req.URL.Path, "/"))
	entry, err := h.getListing(req.Context(), urlPath)
	if err != nil {
		logrus.WithError(err).Errorf("error listing %s", urlPath)
		if errors.Is(err, fs.ErrNotExist) {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		io.WriteString(w, err.Error())
		return
	}

	err = h.template.Execute(w, entry)
	if err != nil {
		fmt.Printf("Failed to render template for %s: %s\n", entry.Name, err)
	}
}
