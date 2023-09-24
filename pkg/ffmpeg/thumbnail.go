/*
 * video-listing Copyright (C) 2023 Mook
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

// Package ffmpeg contains methods for thumbnailing and streaming packaging.
package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/sirupsen/logrus"
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
