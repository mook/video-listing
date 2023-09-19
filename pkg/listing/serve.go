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
	"time"

	"github.com/mook/video-listing/pkg/ffmpeg"
	"github.com/mook/video-listing/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// The length of a path part in the URL, in characters
	tokenLength = crc64.Size * 2
)

type entryType int

const (
	entryTypeVideo entryType = iota // Entry is a video file
	entryTypeDir                    // Entry is a directory
	entryTypeOther                  // Entry is a non-video file
)

var (
	hashTable = crc64.MakeTable(crc64.ECMA)
)

// A directoryListing holds information needed to render a page
type directoryListing struct {
	Name        string   // The name of this directory
	Path        string   // The path to this directory
	Directories []string // Any subdirectories
	Files       []string // Any files in this directory
}

func hashName(name string) string {
	hash := crc64.Checksum([]byte(name), hashTable)
	return fmt.Sprintf("%0*x", tokenLength, hash)
}

type listingStatments struct {
	insert        *sql.Stmt // Insert a new entry
	setThumbnail  *sql.Stmt // Update the thumbnail
	queryPath     *sql.Stmt // Query for one entry
	queryChildren *sql.Stmt // Query for entries
	queryAll      *sql.Stmt // Get all entries (for background tasks)
	access        *sql.Stmt // Update the last accessed time of an entry
	delete        *sql.Stmt // Remove a given entry
}

type ListingHandler struct {
	template *template.Template
	dbConn   *sql.Conn
	stmts    listingStatments
}

func NewListingHandler(ctx context.Context, resources fs.FS, conn *sql.Conn) (*ListingHandler, error) {
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
	result := &ListingHandler{
		template: tmpl,
		dbConn:   conn,
		stmts:    stmts,
	}
	go result.scanVideos(ctx)
	return result, nil
}

func createDatabase(ctx context.Context, conn *sql.Conn) (listingStatments, error) {
	result := listingStatments{}
	_, err := conn.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS listing_cache (
			parent TEXT NOT NULL COLLATE NOCASE, -- URL path of parent
			hash TEXT NOT NULL COLLATE NOCASE,   -- Hash of this entry
			path TEXT NOT NULL COLLATE NOCASE,   -- Absolute file name
			type INT CHECK (type IN (%d, %d, %d)), -- Type of this entry
			last_used INT NOT NULL,
			thumbnail BLOB,
			PRIMARY KEY (parent, hash)
		) STRICT
	`, entryTypeVideo, entryTypeDir, entryTypeOther))
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
		INSERT INTO listing_cache
			(parent, hash, path, type, last_used)
			VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT DO UPDATE SET
			path = ?3,
			last_used = ?5
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing insert: %w", err)
	}
	result.setThumbnail, err = conn.PrepareContext(ctx, `
		UPDATE listing_cache
		SET thumbnail = ?, type = ?, last_used = unixepoch('now', 'utc')
		WHERE parent = ? AND hash = ?
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing thumbnail set: %w", err)
	}
	result.queryPath, err = conn.PrepareContext(ctx, `
		SELECT path FROM listing_cache WHERE parent = ? AND hash = ? LIMIT 1
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing name query: %w", err)
	}
	result.queryChildren, err = conn.PrepareContext(ctx, fmt.Sprintf(`
		SELECT path, type
		FROM listing_cache
		WHERE parent = ? AND hash != "" AND type != %d ORDER BY path ASC
	`, entryTypeOther))
	if err != nil {
		return result, fmt.Errorf("error preparing children query: %w", err)
	}
	result.queryAll, err = conn.PrepareContext(ctx, `
		SELECT parent, hash, path, type, thumbnail NOT NULL, last_used
		FROM listing_cache
		ORDER BY last_used DESC
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing caching update query: %w", err)
	}
	result.access, err = conn.PrepareContext(ctx, `
		UPDATE listing_cache SET last_used = ?
		WHERE parent = ? AND hash = ?
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing access: %w", err)
	}
	result.delete, err = conn.PrepareContext(ctx, `
		DELETE FROM listing_cache WHERE parent = ? AND hash = ?
	`)
	if err != nil {
		return result, fmt.Errorf("error removing stale entry: %w", err)
	}
	_, _ = result.insert.Exec("", "", "/media", 1, 0)
	return result, nil
}

// getListing returns a directory listing.  The parent is the clean URL path,
// all in lower case hash chunks joined by slash ("/") without any preceding or
// trailing slashes.
func (h *ListingHandler) getListing(ctx context.Context, parent string) (*directoryListing, error) {
	result := &directoryListing{Path: parent}

	grandParent, leaf := utils.CutLastString(parent, "/")
	row := h.stmts.queryPath.QueryRowContext(ctx, grandParent, leaf)
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
		var typ entryType
		if err = rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("error reading row: %w", err)
		}
		name = path.Base(name)
		switch typ {
		case entryTypeDir:
			result.Directories = append(result.Directories, name)
		case entryTypeVideo:
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

	readTime := time.Now().Unix()
	children, err := dir.ReadDir(0)
	if err != nil {
		return nil, fmt.Errorf("could not list child entries: %w", err)
	}

	slices.SortFunc(children, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	result := &directoryListing{
		Name: dir.Name(),
		Path: urlPath,
	}
	for _, entry := range children {
		entryTime := readTime
		entryType := entryTypeVideo
		if entry.IsDir() {
			result.Directories = append(result.Directories, entry.Name())
			// Use time 0 so we will walk the directory later
			entryTime = 0
			entryType = entryTypeDir
		} else {
			result.Files = append(result.Files, entry.Name())
		}
		// Insert into the cache, ignoring errors.
		_, err = h.stmts.insert.ExecContext(ctx,
			urlPath,
			hashName(entry.Name()),
			path.Join(dir.Name(), entry.Name()),
			entryType,
			entryTime)
		if err != nil {
			logrus.WithError(err).Error("failed to insert cache")
		}
	}

	parent, hash := utils.CutLastString(urlPath, "/")
	_, _ = h.stmts.access.ExecContext(ctx, readTime, parent, hash)

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

// scanVideos will be run in the background to supply thumbnail images as well
// as to remove stale entries.
func (h *ListingHandler) scanVideos(ctx context.Context) {
	for {
		func() {
			rows, err := h.stmts.queryAll.QueryContext(ctx)
			if err != nil {
				logrus.WithError(err).Error("failed to get cached entries")
				time.Sleep(time.Second)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var parent, hash, path string
				var typ entryType
				var hasThumbnail bool
				var lastUsed int64

				time.Sleep(time.Second)
				err = rows.Scan(&parent, &hash, &path, &typ, &hasThumbnail, &lastUsed)
				if err != nil {
					logrus.WithError(err).Info("Skipping invalid row")
					continue
				}

				_, err := os.Stat(path)
				if errors.Is(err, os.ErrNotExist) {
					// This entry no longer exists
					_, _ = h.stmts.delete.ExecContext(ctx, parent, hash)
					continue
				} else if err != nil {
					logrus.WithError(err).Info("Error checking file")
					continue
				}

				switch typ {
				case entryTypeDir:
					if time.Unix(lastUsed, 0).Add(time.Hour).Before(time.Now()) {
						// This directory entry is more than an hour old; scan it.
						h.readDirectory(ctx, strings.Trim(parent+"/"+hash, "/"))
					}
				case entryTypeVideo:
					if !hasThumbnail {
						buffer, err := ffmpeg.CreateThumbnail(ctx, path)
						if err != nil {
							logrus.WithError(err).WithField("path", path).Info("failed to create thumbnail")
							_, err = h.stmts.setThumbnail.ExecContext(ctx, nil, entryTypeOther, parent, hash)
							if err != nil {
								logrus.WithError(err).Debug("failed to set file as invalid")
							}
						} else {
							_, err = h.stmts.setThumbnail.ExecContext(ctx, buffer, entryTypeVideo, parent, hash)
							if err != nil {
								logrus.WithError(err).Debug("failed to set thumbnail")
							}
						}
					}
				}
			}
		}()
	}
}

func (h *ListingHandler) ServeVideo(w http.ResponseWriter, req *http.Request) {
	urlPath := strings.ToLower(strings.Trim(req.URL.Path, "/"))
	playlistPath := path.Join("/cache", urlPath, ffmpeg.PlaylistName)
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

	parent, hash := utils.CutLastString(urlPath, "/")
	var filePath string
	err = h.stmts.queryPath.QueryRowContext(req.Context(), parent, hash).Scan(&filePath)
	if err != nil {
		logrus.WithError(err).WithField("path", urlPath).Debug("Could not find video")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, fmt.Sprintf("Could not find %s", urlPath))
		return
	}

	result, err := ffmpeg.PackageForStreaming(req.Context(), urlPath, filePath)
	if err != nil {
		logrus.WithError(err).WithField("path", urlPath).Error("Error transcoding")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Error transcoding %s: %s", urlPath, err))
	} else {
		http.ServeFile(w, req, result)
	}
}
