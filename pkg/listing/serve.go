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
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
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
	queryName     *sql.Stmt // Query for one entry
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
			last_used INT DEFAULT (unixepoch('now', 'utc')),
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
		INSERT OR REPLACE INTO listing_cache (parent, hash, path, type) VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing insert: %w", err)
	}
	result.setThumbnail, err = conn.PrepareContext(ctx, `
		UPDATE listing_cache SET thumbnail = ?, type = ? WHERE parent = ? AND hash = ?
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing thumbnail set: %w", err)
	}
	result.queryName, err = conn.PrepareContext(ctx, `
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
		SELECT parent, hash, path, type, thumbnail NOT NULL
		FROM listing_cache
		ORDER BY last_used DESC
	`)
	if err != nil {
		return result, fmt.Errorf("error preparing caching update query: %w", err)
	}
	result.access, err = conn.PrepareContext(ctx, `
		UPDATE listing_cache SET last_used = unixepoch("now", "utc")
		WHERE parent = ?
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

// scanVideos will be run in the background to supply thumbnail images as well
// as to remove stale entries.
func (h *ListingHandler) scanVideos(ctx context.Context) {
	for {
		rows, err := h.stmts.queryAll.QueryContext(ctx)
		if err != nil {
			logrus.WithError(err).Error("failed to get cached entries")
			time.Sleep(time.Second)
			continue
		}
		for rows.Next() {
			var parent, hash, path string
			var typ entryType
			var hasThumbnail bool

			time.Sleep(time.Second)
			err = rows.Scan(&parent, &hash, &path, &typ, &hasThumbnail)
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
				// TODO: refresh in the background
			case entryTypeVideo:
				if !hasThumbnail {
					if err = h.makeThumbnail(ctx, parent, hash, path); err != nil {
						logrus.WithError(err).Info("Failed to make thumbnail")
					}
				}
			}
		}
	}
}

// scaleSample scales the input sample to given dimensions.
// This is just video.ConvertSample with more details.
func scaleSample(ctx context.Context, sample *gst.Sample, width, height int) (*gst.Sample, error) {
	pipeline, err := gst.NewPipelineFromString(fmt.Sprintf(`
		appsrc name=src ! videoconvertscale ! video/x-raw,width=%d,height=%d ! jpegenc ! appsink name=sink
	`, width, height))
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}
	srcElement, err := pipeline.GetElementByName("src")
	if err != nil {
		return nil, fmt.Errorf("failed to get source: %w", err)
	}
	src := app.SrcFromElement(srcElement)
	if flowReturn := src.PushSample(sample); flowReturn != gst.FlowOK {
		return nil, fmt.Errorf("pushing sample returned %s", &flowReturn)
	}
	sinkElement, err := pipeline.GetElementByName("sink")
	if err != nil {
		return nil, fmt.Errorf("failed to get sink: %w", err)
	}
	sink := app.SinkFromElement(sinkElement)

	// preroll
	if err = pipeline.SetState(gst.StatePaused); err != nil {
		return nil, fmt.Errorf("failed to pause pipeline: %w", err)
	}
	defer pipeline.SetState(gst.StateNull)

	sample = sink.PullPreroll()
	if sample == nil {
		return nil, fmt.Errorf("failed to get sample")
	}

	return sample, nil
}

// makeThumbnail creates a thumbnail for a cache entry.
func (h *ListingHandler) makeThumbnail(ctx context.Context, parent, hash, path string) error {
	// This is partially based on:
	// https://cgit.freedesktop.org/gstreamer/gstreamer/tree/subprojects/gst-plugins-base/tests/examples/snapshot/snapshot.c

	logrus.WithField("path", path).Trace("Creating thumbnail...")

	// Construct a pipeline to decode the file
	u := &url.URL{Scheme: "file", Path: path}
	pipeline, err := gst.NewPipelineFromString(fmt.Sprintf(`
		uridecodebin uri=%s ! videoconvertscale ! appsink name=sink
	`, u.String()))
	if err != nil {
		return fmt.Errorf("failed to make pipeline for %s: %w", path, err)
	}

	// Get the sink element from the pipeline
	sinkElement, err := pipeline.GetElementByName("sink")
	if err != nil {
		return fmt.Errorf("failed to get sink for %s: %w", path, err)
	}
	sink := app.SinkFromElement(sinkElement)

	// Pause the pipeline (preroll)
	if err = pipeline.SetState(gst.StatePaused); err != nil {
		return fmt.Errorf("failed to pause pipeline %s: %w", path, err)
	}
	stateResult, _ := pipeline.GetState(gst.StatePaused, gst.ClockTime(5*time.Second))
	if stateResult == gst.StateChangeFailure {
		_, err = h.stmts.setThumbnail.ExecContext(ctx, nil, entryTypeOther, parent, hash)
		if err != nil {
			logrus.WithError(err).Errorf("failed to disable entry %s", path)
		}
		return fmt.Errorf("failed to preroll pipeline %s", path)
	}
	defer pipeline.SetState(gst.StateNull)

	// Seek to a time
	seekOffset := int64(time.Second)
	ok, duration := pipeline.QueryDuration(gst.FormatTime)
	if ok && duration > 0 {
		seekOffset = duration * 40 / 100
	}
	// We don't seem to have gst_element_seek_simple or even gst_element_seek;
	// implement it manually with events.
	_ = pipeline.SendEvent(gst.NewSeekEvent(
		1.0, // rate
		gst.FormatTime,
		gst.SeekFlagFlush|gst.SeekFlagKeyUnit|gst.SeekFlagSnapNearest,
		gst.SeekTypeSet, // start type
		seekOffset,      // start position
		gst.SeekTypeEnd, // stop type
		0,               // stop position
	))

	// Get the sample for the thumbnail image
	sample := sink.PullPreroll()
	if sample == nil {
		return fmt.Errorf("failed to get sample for %s", path)
	}

	// Scale the image while keeping the aspect ratio
	structure := sample.GetCaps().GetStructureAt(0)
	width, errWidth := structure.GetValue("width")
	height, errHeight := structure.GetValue("height")
	if errWidth != nil || errHeight != nil {
		logrus.WithFields(logrus.Fields{"width": errWidth, "height": errHeight}).Error("could not get sample size")
	} else {
		desiredHeight := int(int64(height.(int)) * 320 / int64(width.(int)))
		sample, err = scaleSample(ctx, sample, 320, desiredHeight)
		if err != nil {
			return fmt.Errorf("failed to scale sample: %w", err)
		}
	}

	_, err = h.stmts.setThumbnail.ExecContext(ctx, sample.GetBuffer().Bytes(), entryTypeVideo, parent, hash)
	if err != nil {
		return fmt.Errorf("failed to set thumbnail for %s: %w", path, err)
	}

	return nil
}
