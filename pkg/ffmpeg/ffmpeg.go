// Package ffmpeg contains methods for thumbnailing and streaming packaging.
package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/multierr"
)

const (
	PlaylistName = "out.mpd"
)

// Metadata for a given file, as provided by ffprobe
type metadata struct {
	Format struct {
		Filename       string            `json:"filename"`
		NbStreams      int               `json:"nb_streams"`
		NbPrograms     int               `json:"nb_programs"`
		FormatName     string            `json:"format_name"`
		FormatLongName string            `json:"format_long_name"`
		StartTime      string            `json:"start_time"`
		Duration       string            `json:"duration"`
		Size           string            `json:"size"`
		BitRate        string            `json:"bit_rate"`
		ProbeScore     int               `json:"probe_score"`
		Tags           map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index     int               `json:"index"`
		CodecType string            `json:"codec_type"`
		Width     int               `json:"width,omitempty"`
		Height    int               `json:"height,omitempty"`
		Tags      map[string]string `json:"tags"`
	} `json:"stream"`
}

// CreateThumbnail creates a JPEG thumbnail for the given path.
func CreateThumbnail(ctx context.Context, filePath string) ([]byte, error) {
	metadata, err := probe(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to probe %s: %w", filePath, err)
	}
	tempPath, err := os.CreateTemp("", "thumbnail-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary thumbnail file: %w", err)
	}
	tempPath.Close()
	os.Remove(tempPath.Name())
	args := []string{"-i", filePath, "-frames:v", "1", tempPath.Name()}
	duration, err := strconv.ParseFloat(metadata.Format.Duration, 64)
	if err != nil {
		logrus.WithError(err).Debug("Failed to convert file duration")
		duration = 0
	}
	if duration > 0 {
		offset := 0.0
		if duration > 10*60 {
			// Video is more than ten minutes; this may be a TV show, avoid the
			// first couple minutes for opening.
			offset = 2.0
			duration -= offset
		}
		targetTime := fmt.Sprintf("%f", offset+duration*0.2)
		args = append([]string{"-ss", targetTime}, args...)
	}
	if _, err = exec.CommandContext(ctx, "ffmpeg", args...).Output(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			logrus.WithError(err).WithFields(logrus.Fields{
				"stderr": string(exitError.Stderr),
				"file":   filePath,
				"args":   args,
			}).Error("Failed to write thumbnail")
		}
		return nil, fmt.Errorf("failed to create thumbnail for %s: %w", filePath, err)
	}
	return os.ReadFile(tempPath.Name())
}

func probe(ctx context.Context, filePath string) (*metadata, error) {
	cmd := exec.CommandContext(ctx,
		"ffprobe", "-loglevel", "error", "-print_format", "json",
		"-show_format", "-show_streams", filePath)
	bufBytes, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			logrus.WithError(err).WithFields(logrus.Fields{
				"stderr": string(exitError.Stderr),
				"file":   filePath,
				"args":   cmd.Args,
			}).Error("Failed to probe file")
		}
		return nil, err
	}
	result := metadata{}
	if err = json.Unmarshal(bufBytes, &result); err != nil {
		logrus.WithError(err).WithField("data", string(bufBytes)).Debug("failed to convert")
		return nil, err
	}
	return &result, nil
}

// Package the given filePath for streaming, assuming the given cache key.
// Returns the path to the playlist file.
//
// Note that the returned playlist file may be incomplete by the time this
// returns; this is to ensure the user can start streaming faster.
func PackageForStreaming(ctx context.Context, key, filePath string) (string, error) {
	var err error
	outDir := path.Join("/cache", key)
	playlistPath := path.Join(outDir, PlaylistName)
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	_ = os.Remove(playlistPath)

	waitForPlaylist := func(cmd *exec.Cmd) bool {
		ch := make(chan bool, 1)
		defer close(ch)
		done := false
		go func() {
			_, err := os.Lstat(playlistPath)
			for !done && errors.Is(err, fs.ErrNotExist) {
				time.Sleep(100 * time.Millisecond)
				_, err = os.Lstat(playlistPath)
			}
			ch <- true
		}()
		go func() {
			_ = cmd.Wait()
			ch <- false
			done = true
		}()
		return <-ch
	}

	args := []string{
		"-i", filePath, "-f", "dash", "-streaming", "1", "-ldash", "1",
		"-single_file_name", "stream-$RepresentationID$.$ext$",
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", append(args, "-codec:v", "copy", PlaylistName)...)
	cmd.Dir = outDir
	if err = cmd.Start(); err != nil {
		return "", multierr.Append(err, os.RemoveAll(outDir))
	}
	if waitForPlaylist(cmd) {
		if _, err = os.Lstat(playlistPath); err != nil {
			return "", multierr.Append(err, os.Remove(playlistPath))
		}
		return playlistPath, nil
	}
	_ = os.Remove(playlistPath) // Remove any broken playlist files
	// Getting here means the ffmpeg command failed to execute.  Try again
	// without -codec:v copy
	cmd = exec.CommandContext(ctx, "ffmpeg", append(args, PlaylistName)...)
	cmd.Dir = outDir
	if err = cmd.Start(); err != nil {
		return "", multierr.Append(err, os.RemoveAll(outDir))
	}
	if waitForPlaylist(cmd) {
		if _, err = os.Lstat(playlistPath); err != nil {
			return "", multierr.Append(err, os.Remove(playlistPath))
		}
		return playlistPath, nil
	}
	// ffmpeg still failed to run; cleanup and return error.
	err = fmt.Errorf("failed to run ffmpeg: %w", cmd.Wait())
	return "", multierr.Append(err, os.RemoveAll(outDir))
}
