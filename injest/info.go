package injest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const infoBaseName = ".info.json"

var mediaExtensions = map[string]struct{}{
	".asf":  {},
	".avi":  {},
	".f4v":  {},
	".flv":  {},
	".mkv":  {},
	".mov":  {},
	".mp4":  {},
	".mpg":  {},
	".ogv":  {},
	".rm":   {},
	".rmvb": {},
	".webm": {},
	".wmv":  {},
}

// InfoType describes the data in `.info.json` files in each directory.
type InfoType struct {
	// The last time injesting for this directory (not its children) was completed.
	Timestamp    time.Time `json:"timestamp"`
	AniListID    int       `json:"anilist,omitempty"`
	NativeTitle  string    `json:"native,omitempty"`
	EnglishTitle string    `json:"english,omitempty"`
	// Mapping of each media file to whether it's marked as seen.
	Seen map[string]bool `json:"seen,omitempty"`
	// Mapping of each child directory to when it was last injested (mtime).
	Injested map[string]time.Time `json:"injested,omitempty"`
	changed  bool
	// Mapping of file/directory name to modification time.
	mtimes map[string]time.Time
}

// ReadInfo reads the saved information from a directory, given as the absolute
// path.  It is not an error if the saved info does not exist.  The Seen and
// Injested maps are filled to contain zero values.
func ReadInfo(directory string) (*InfoType, error) {
	infoPath := filepath.Join(directory, infoBaseName)
	info := InfoType{
		Seen:     make(map[string]bool),
		Injested: make(map[string]time.Time),
		mtimes:   make(map[string]time.Time),
	}
	migrate := false
	migratingSeen := make(map[string]bool)
	f, err := os.Open(infoPath)
	if err == nil {
		defer f.Close()
		if err := json.NewDecoder(f).Decode(&info); err != nil {
			return nil, fmt.Errorf("failed to load saved info: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else {
		migrate = true
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			if migrate && len(name) > 5 && strings.HasSuffix(name, ".seen") {
				name := name[1 : len(name)-5]
				migratingSeen[name] = true
			}
			continue
		}
		if entry.IsDir() {
			if name == "@eaDir" {
				delete(info.Injested, name)
				continue
			}
			if _, ok := info.Injested[name]; !ok {
				info.Injested[name] = time.Time{}
				info.changed = true
			}
			seen[name] = true
			if stat, err := entry.Info(); err == nil {
				info.mtimes[name] = stat.ModTime()
			}
		} else if entry.Type().IsRegular() {
			if _, ok := mediaExtensions[strings.ToLower(filepath.Ext(name))]; !ok {
				continue // Not a media file
			}
			if _, ok := info.Seen[name]; !ok {
				info.Seen[name] = false
				info.changed = true
			}
			seen[name] = true
			if stat, err := entry.Info(); err == nil {
				info.mtimes[name] = stat.ModTime()
			}
		}
	}

	if migrate {
		for name := range info.Seen {
			if migratingSeen[name] {
				info.Seen[name] = true
			}
		}
	}

	for dir := range info.Injested {
		if !seen[dir] {
			delete(info.Injested, dir)
			info.changed = true
		}
	}
	for file := range info.Seen {
		if !seen[file] {
			delete(info.Seen, file)
			info.changed = true
		}
	}

	return &info, nil
}

func WriteInfo(directory string, info *InfoType) error {
	infoPath := filepath.Join(directory, infoBaseName)
	f, err := os.CreateTemp(directory, infoBaseName)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := json.NewEncoder(f).Encode(info); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), infoPath); err != nil {
		return err
	}

	return nil
}
