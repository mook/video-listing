// Package thumbnail generates a thumbnail for a video file by spawning ffmpeg.
package thumbnail

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Given the path if a video file, create a thumbnail at the given path.
func Create(ctx context.Context, videoPath, thumbnailPath string) error {
	duration, err := getDuration(ctx, videoPath)
	if err != nil {
		return err
	}
	var timeCodes []time.Duration
	if duration > 10*time.Minute {
		// If a video is more than ten minutes, there is a good chance that this is
		// a TV show or similar; avoid the first and last couple minutes for opening
		// and ending.
		offset := (duration - 4*time.Minute) / 5
		for t := 2 * time.Minute; t < duration-2*time.Minute; t += offset {
			timeCodes = append(timeCodes, t)
		}
	} else {
		for t := time.Duration(0); t < duration; t += duration / 5 {
			timeCodes = append(timeCodes, t)
		}
	}

	best := &bytes.Buffer{}
	for _, t := range timeCodes {
		candidate, err := getFrame(ctx, videoPath, float64(t)/float64(time.Second))
		if err != nil {
			logrus.WithError(err).WithField("path", videoPath).Error("Failed to generate thumbnail")
		} else if candidate.Len() > best.Len() {
			best = candidate
		}
	}

	if best.Len() < 1 {
		return fmt.Errorf("failed to generate thumbnail")
	}

	if err := os.WriteFile(thumbnailPath, best.Bytes(), 0o644); err != nil {
		_ = os.Remove(thumbnailPath)
		return err
	}
	return nil
}

// Get the duration of a video file.
func getDuration(ctx context.Context, videoPath string) (time.Duration, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-loglevel", "quiet",
		"-show_entries", "format=duration",
		"-output_format", "default=nokey=1:noprint_wrappers=1",
		videoPath)
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	result, err := strconv.ParseFloat(strings.TrimSpace(buf.String()), 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(result * float64(time.Second)), nil
}

func getFrame(ctx context.Context, videoPath string, timeCode float64) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "quiet",
		"-ss", fmt.Sprintf("%f", timeCode),
		"-t", "10",
		"-i", videoPath,
		"-filter:v", "select=eq(pict_type\\,I),thumbnail",
		"-frames:v", "1",
		"-f", "mjpeg", "-")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &buf, nil
}
