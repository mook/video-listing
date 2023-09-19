package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/multierr"
)

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

	logrus.WithField("playlist", playlistPath).Trace("transcoding...")

	staticArgs := []string{
		"-i", filePath, "-f", "dash", "-streaming", "1", "-ldash", "1",
		"-init_seg_name", "stream-$RepresentationID$-init.$ext$",
		"-media_seg_name", "stream-$RepresentationID$-chunk-$Number%05d$.$ext$",
	}
	maybeArgs := [][]string{
		{"-codec:v", "copy", "-codec:a", "copy"},
		{"-codec:v", "copy"},
		{},
	}

	for _, maybeArg := range maybeArgs {
		args := append(append(staticArgs, maybeArg...), PlaylistName)
		// Run the transcode in a background context so it doesn't get killed
		// when the initial HTTP session is complete.
		cmd := exec.CommandContext(context.Background(), "ffmpeg", args...)
		log := logrus.WithFields(logrus.Fields{
			"path": filePath,
			"args": args,
		})
		log.Trace("running ffmpeg...")
		cmd.Dir = outDir
		stdout := &bytes.Buffer{}
		cmd.Stdout = stdout
		stderr := &bytes.Buffer{}
		cmd.Stderr = stderr
		if err = cmd.Start(); err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				log = log.WithField("stderr", string(exitError.Stderr))
			}
			log.WithError(err).Trace("Failed to start command")
			continue
		}
		exited := atomic.Bool{}
		go func() {
			err := cmd.Wait()
			exited.Store(true)
			if err != nil {
				log = logrus.WithError(err)
				if stdout.Len() > 0 {
					log = log.WithField("stdout", stdout.String())
				}
				if stderr.Len() > 0 {
					log = log.WithField("stderr", stderr.String())
				}
				log.Error("Failed to transcode")
			} else {
				if stdout.Len() > 0 {
					log = log.WithField("stdout", stdout.String())
				}
				log.Trace("transcode finished")
			}
		}()
		for !exited.Load() {
			_, err = os.Lstat(playlistPath)
			if err == nil {
				log.Trace("Found playlist file")
				return playlistPath, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				cmd.Process.Signal(os.Interrupt)
				log.WithError(err).Trace("Failed to stat")
				return "", fmt.Errorf("failed to run ffmpeg: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
		}
	}

	// ffmpeg still failed to run; cleanup and return error.
	err = fmt.Errorf("failed to run ffmpeg: %w", err)
	return "", multierr.Append(err, os.RemoveAll(outDir))
}
