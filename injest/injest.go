// Package injest processes directories to find information about media.
package injest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mook/video-listing/thumbnail"
	"github.com/sirupsen/logrus"
)

type task interface {
	Process(ctx context.Context) error
}

// Injester is the main object doing the injesting.  It must be created via
// a call to New.
type Injester struct {
	// The root directory, from with all paths are relative to.
	root    string
	cond    *sync.Cond
	pending []task
}

// Create a new Injester.
func New(root string) *Injester {
	return &Injester{
		root: root,
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

type QueueOptions struct {
	// Directory relative to the media root for processing
	Directory string
	// Override AniList ID
	ID int
	// Force rescan; ignored if ID is set.
	Force bool
}

type Queue func(QueueOptions)

// Queue a single directory relative to the media root for processing, locating
// information about the media contained therein.
func (i *Injester) Queue(opts QueueOptions) {
	// Check that the directory is relative
	if opts.Directory != "." {
		absPath := filepath.Clean(filepath.Join(i.root, opts.Directory))
		expectedRoot := fmt.Sprintf("%s%c", filepath.Clean(i.root), filepath.Separator)
		if !strings.HasPrefix(absPath, expectedRoot) {
			logrus.WithField("path", absPath).Error("Rejecting injester queue: invalid path")
			return // Absolute path does not start with root
		}
	}
	i.queue(&injestDirectory{
		i:            i,
		QueueOptions: opts,
	})
}

// queue a task for processing; the type of task may vary.
func (i *Injester) queue(task task) {
	i.cond.L.Lock()
	defer i.cond.L.Unlock()

	i.pending = append(i.pending, task)
	i.cond.Signal()
	logrus.WithField("task", task).Debug("Injester queued item")
}

type injestDirectory struct {
	i *Injester
	QueueOptions
}

func (d *injestDirectory) absPath() string {
	return filepath.Join(d.i.root, d.Directory)
}

func (d *injestDirectory) String() string {
	return fmt.Sprintf("<injest %s>", d.Directory)
}

func (d *injestDirectory) Process(ctx context.Context) error {
	log := logrus.WithField("directory", d.Directory)
	log.Debug("Scanning directory")

	entries, err := os.ReadDir(d.absPath())
	if err != nil {
		return err
	}

	var lastTime time.Time
	directories := make(map[string]time.Time)
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // Skip hidden files and directories.
		}
		info, err := entry.Info()
		if err != nil {
			log.WithError(err).WithField("entry", name).Error("Failed to read directory info")
			continue
		}
		if entry.IsDir() {
			if name == "@eaDir" {
				continue
			}
			directories[name] = info.ModTime()
			if info.ModTime().After(lastTime) {
				lastTime = info.ModTime()
			}
		} else if entry.Type().IsRegular() {
			if _, ok := mediaExtensions[strings.ToLower(filepath.Ext(name))]; !ok {
				continue // Not a media file
			}
			files = append(files, name)
			if info.ModTime().After(lastTime) {
				lastTime = info.ModTime()
			}
		}
	}

	info, err := ReadInfo(d.absPath(), true)
	log.WithError(err).WithField("info", info).Debug("Read existing info")
	if err != nil {
		return err
	}

	if d.Force || d.ID != info.AniListID || len(info.Seen) > 0 {
		// This is a media directory; look up what it is.
		if d.ID != 0 {
			idChanged := info.AniListID != d.ID
			info.AniListID = d.ID
			err = d.i.requestInfo(ctx, d.absPath(), info, d.Force || idChanged, true)
		} else {
			err = d.i.requestInfo(ctx, d.absPath(), info, d.Force, false)
		}
		log.WithError(err).WithField("info", info).Debug("Requested info")
		// Ignore any errors here; we can rescan later.
	}

	if d.Force || lastTime.After(info.Timestamp) {
		info.changed = true
		info.Timestamp = lastTime

		for _, child := range files {
			d.i.queue(&createThumbnail{
				i:       d.i,
				absPath: filepath.Join(d.absPath(), child),
			})
		}
	}

	// Update the subdirectories
	for d := range info.Injested {
		if _, ok := directories[d]; !ok {
			delete(info.Injested, d)
		}
	}
	for child, t := range directories {
		if t.After(info.Injested[child]) {
			d.i.queue(&injestDirectory{
				i: d.i,
				QueueOptions: QueueOptions{
					Directory: filepath.Join(d.Directory, child),
				},
			})
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

		err = WriteInfo(d.absPath(), info)
		log.WithError(err).WithField("info", info).Debug("Wrote info")
		if err != nil {
			return err
		}
	} else {
		log.Debugf("Skipping unchanged info: %+v", info)
	}

	return nil
}

type createThumbnail struct {
	i       *Injester
	absPath string
}

func (t *createThumbnail) String() string {
	return fmt.Sprintf("<thumbnail %s>", t.absPath)
}

func (t *createThumbnail) Process(ctx context.Context) error {
	parent, base := filepath.Split(t.absPath)
	thumbPath := filepath.Join(parent, fmt.Sprintf(".%s.webp", base))
	err := thumbnail.Create(ctx, t.absPath, thumbPath)
	if err != nil {
		return err
	}
	// Remove the old jpeg thumbnail if it exists.
	_ = os.Remove(filepath.Join(parent, fmt.Sprintf(".%s.jpg", base)))
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
			task := func() task {
				i.cond.L.Lock()
				defer i.cond.L.Unlock()
				for len(i.pending) == 0 {
					i.cond.Wait()
				}
				var task task
				i.pending, task = i.pending[:len(i.pending)-1], i.pending[len(i.pending)-1]
				return task
			}()
			err := task.Process(ctx)
			if err != nil {
				logrus.WithError(err).WithField("task", task).Error("failed to injest directory")
			}
		}
	}
}
