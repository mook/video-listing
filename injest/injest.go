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

// Queue a single directory relative to the media root for processing, locating
// information about the media contained therein.
func (i *Injester) Queue(directory string) {
	i.queue(&injestDirectory{
		i:       i,
		absPath: filepath.Clean(filepath.Join(i.root, directory)),
		force:   true,
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
	i       *Injester
	absPath string
	force   bool
}

func (d *injestDirectory) String() string {
	return fmt.Sprintf("<injest %s>", d.absPath)
}

func (d *injestDirectory) Process(ctx context.Context) error {
	log := logrus.WithField("directory", d.absPath)
	log.Debug("Scanning directory")

	entries, err := os.ReadDir(d.absPath)
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

	info, err := ReadInfo(d.absPath, true)
	log.WithError(err).WithField("info", info).Debug("Read existing info")
	if err != nil {
		return err
	}

	if len(info.Seen) > 0 {
		// This is a media directory; look up what it is.
		err = d.i.requestInfo(ctx, d.absPath, info, d.force)
		log.WithError(err).WithField("info", info).Debug("Requested info")
		// Ignore any errors here; we can rescan later.
	}

	if d.force || lastTime.After(info.Timestamp) {
		info.changed = true
		info.Timestamp = lastTime

		for _, child := range files {
			d.i.queue(&createThumbnail{
				i:       d.i,
				absPath: filepath.Join(d.absPath, child),
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
				i:       d.i,
				absPath: filepath.Join(d.absPath, child),
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

		err = WriteInfo(d.absPath, info)
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
	thumbPath := filepath.Join(parent, fmt.Sprintf(".%s.jpg", base))
	err := thumbnail.Create(ctx, t.absPath, thumbPath)
	if err != nil {
		return err
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
				return task.Process(ctx)
			}()
			if err != nil {
				logrus.WithError(err).Error("failed to injest directory")
			}
		}
	}
}
