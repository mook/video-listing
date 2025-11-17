// Package injest processes directories to find information about media.
package injest

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Injester is the main object doing the injesting.  It must be created via
// a call to New.
type Injester struct {
	// The root directory, from with all paths are relative to.
	root  string
	cond  *sync.Cond
	queue []string
}

// Create a new Injester.
func New(root string) *Injester {
	return &Injester{
		root: root,
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

// Queue a single directory relative to the media root for processing, locating
// information about the media contained therein.
func (i *Injester) Queue(directory string) {
	i.cond.L.Lock()
	defer i.cond.L.Unlock()

	i.queue = append(i.queue, directory)
	i.cond.Signal()
	logrus.WithField("directory", directory).Debug("Injester queued item")
}

// injest processes a single directory, relative to the media root.
func (i *Injester) injest(ctx context.Context, directory string) error {
	absPath := filepath.Clean(filepath.Join(i.root, directory))
	log := logrus.WithField("directory", absPath)
	log.Debug("Scanning directory")

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return err
	}

	var lastTime time.Time
	directories := make(map[string]time.Time)
	var files []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue // Skip hidden files and directories.
		}
		info, err := entry.Info()
		if err != nil {
			log.WithError(err).WithField("entry", entry.Name()).Error("Failed to read directory info")
			continue
		}
		if entry.IsDir() {
			directories[entry.Name()] = info.ModTime()
			if info.ModTime().After(lastTime) {
				lastTime = info.ModTime()
			}
		} else if entry.Type().IsRegular() {
			if _, ok := mediaExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
				continue // Not a media file
			}
			files = append(files, entry.Name())
			if info.ModTime().After(lastTime) {
				lastTime = info.ModTime()
			}
		}
	}

	info, err := ReadInfo(filepath.Join(i.root, directory))
	log.WithError(err).WithField("info", info).Debug("Read existing info")
	if err != nil {
		return err
	}

	if len(info.Seen) > 0 {
		// This is a media directory; look up what it is.
		err = i.requestInfo(ctx, directory, info)
		log.WithError(err).WithField("info", info).Debug("Requested info")
		// Ignore any errors here; we can rescan later.
	}

	if lastTime.After(info.Timestamp) {
		info.changed = true
		info.Timestamp = lastTime
	}

	// Update the subdirectories
	for d := range info.Injested {
		if _, ok := directories[d]; !ok {
			delete(info.Injested, d)
		}
	}
	for d, t := range directories {
		if t.After(info.Injested[d]) {
			i.Queue(path.Join(directory, d))
			info.changed = true
		}
	}

	if info.changed {
		// Update the last modified time
		for _, t := range info.mtimes {
			if t.After(info.Timestamp) {
				info.Timestamp = t
			}
		}

		err = WriteInfo(filepath.Join(i.root, directory), info)
		log.WithError(err).WithField("info", info).Debug("Wrote info")
		if err != nil {
			return err
		}
	}

	return nil
}

// Run the injester; this returns if the context is closed, or a fatal error
// was encountered.
func (i *Injester) Run(ctx context.Context) error {
	logrus.WithField("root", i.root).Debug("Injester waiting for items")
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			err := func() error {
				directory := func() string {
					i.cond.L.Lock()
					defer i.cond.L.Unlock()
					for len(i.queue) == 0 {
						i.cond.Wait()
					}
					var directory string
					i.queue, directory = i.queue[:len(i.queue)-1], i.queue[len(i.queue)-1]
					return directory
				}()
				return i.injest(ctx, directory)
			}()
			if err != nil {
				logrus.WithError(err).Error("failed to injest directory")
			}
		}
	}
}
