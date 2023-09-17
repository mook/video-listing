package transcoder

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/go-gst/go-gst/gst"
	"github.com/sirupsen/logrus"
	"go.uber.org/multierr"
)

const (
	// PlaylistName is the name of the playlist file
	PlaylistName = "playlist.mpd"
)

// Transcode media for use with Chromecast.  Key is the URL path, and filePath
// is the name of the file to transcode.  Returns the path to the playlist file.
func Transcode(key, filePath string) (_ string, err error) {
	t := &transcoder{errors: make(chan error)}

	defer multierr.AppendFunc(&err, t.cleanup)

	if multierr.AppendInto(&err, t.beginTranscode(key, filePath)) {
		return
	}

	resultError := <-t.errors
	if multierr.AppendInto(&err, resultError) {
		return
	}

	result := path.Join("/cache", key)
	if !multierr.AppendInto(&err, os.Rename(t.workDir, result)) {
		t.workDir = ""
		return path.Join(result, PlaylistName), nil
	}

	return
}

type transcoder struct {
	demux    *gst.Pipeline
	sink     *gst.Element
	hasVideo bool
	hasAudio bool
	linked   bool
	workDir  string
	errors   chan error
}

// Start the transcode process
func (t *transcoder) beginTranscode(key, filePath string) error {
	logrus.WithFields(logrus.Fields{"key": key, "path": filePath}).Trace("Transcoding...")

	outDir := path.Join("/cache", key+".tmp")
	err := os.MkdirAll(outDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to make temporary directory for transcoding: %w", err)
	}
	t.workDir = outDir

	// The caps we allow are derived from Chromecast specs:
	// https://developers.google.com/cast/docs/media

	pipeline, err := gst.NewPipelineFromString(`
			filesrc name=src
			! decodebin name=decodebin expose-all-streams=false caps="
				video/x-raw; video/x-h264; video/x-h265; video/x-vp8; video/x-vp9;
				audio/x-raw; audio/mpeg(mpegversion=1;layer=3);
				audio/mpeg(mpegversion=2); audio/mpeg(mpegversion=4);
				audio/x-vorbis; audio/x-opus
				"
			! dashsink name=sink muxer=mp4
		`)

	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}
	if src, err := pipeline.GetElementByName("src"); err != nil {
		return fmt.Errorf("failed to get src: %w", err)
	} else if err = src.Set("location", filePath); err != nil {
		return fmt.Errorf("failed to set location: %w", err)
	}

	pipeline.GetPipelineBus().AddWatch(func(msg *gst.Message) bool {
		switch msg.Type() {
		case gst.MessageError:
			err := msg.ParseError()
			logrus.WithError(err).WithField("debug", err.DebugString()).Error("error message on bus")
		}
		return true
	})

	if decodebin, err := pipeline.GetElementByName("decodebin"); err != nil {
		return fmt.Errorf("failed to get decodebin: %w", err)
	} else if _, err = decodebin.Connect("pad-added", t.onPadAdded); err != nil {
		return fmt.Errorf("failed to listen pad-added: %w", err)
	} else if _, err = decodebin.Connect("no-more-pads", t.onNoMorePads); err != nil {
		return fmt.Errorf("failed to listen no-more-pads: %w", err)
	}

	if sink, err := pipeline.GetElementByName("sink"); err != nil {
		return fmt.Errorf("failed to get sink: %w", err)
	} else if err = sink.Set("mpd-root-path", outDir); err != nil {
		return fmt.Errorf("failed to set sink root path: %w", err)
	} else if err = sink.Set("mpd-filename", PlaylistName); err != nil {
		return fmt.Errorf("failed to set sink playlist location: %w", err)
	} else if err = sink.Set("mpd-baseurl", "/v/"+key); err != nil {
		return fmt.Errorf("failed to set sink base url: %w", err)
	} else {
		t.sink = sink
	}

	t.demux = pipeline

	if err = pipeline.Start(); err != nil {
		return fmt.Errorf("failed to preroll: %w", err)
	}

	return nil
}

func (t *transcoder) onPadAdded(decodeBin *gst.Element, srcPad *gst.Pad) {
	describePad := func(pad *gst.Pad) string {
		if caps := pad.GetCurrentCaps(); caps != nil {
			return caps.String()
		}
		return "<unknown caps>"
	}

	var padName string
	if caps := srcPad.GetCurrentCaps(); caps != nil {
		for i := 0; i < caps.GetSize(); i++ {
			mediaType := caps.GetStructureAt(i).Name()
			if strings.HasPrefix(mediaType, "audio/") {
				t.hasAudio = true
				padName = "audio_%u"
			} else if strings.HasPrefix(mediaType, "video/") {
				t.hasVideo = true
				padName = "video_%u"
			}
		}
	}
	logrus.WithFields(logrus.Fields{
		"decodebin": decodeBin,
		"pad":       srcPad,
		"sink":      t.sink,
		"caps":      describePad(srcPad),
		"pad type":  padName,
	}).Trace("pad was added")
	if padName == "" {
		return
	}
	if true {
		return
	}
	destPad := t.sink.GetRequestPad(padName)
	if destPad == nil {
		logrus.Error("failed to create request pad")
		return
	}
	result := srcPad.Link(destPad)
	logrus.WithFields(logrus.Fields{
		"src":    describePad(srcPad),
		"dest":   describePad(destPad),
		"return": result,
	}).Trace("Tried to link pad")
}

func (t *transcoder) onNoMorePads(decodeBin *gst.Element) {
	logrus.Trace("no more pads")
}

func (t *transcoder) cleanup() error {
	var err error
	if t.demux != nil {
		t.demux.SetState(gst.StateNull)
	}
	if t.workDir != "" {
		multierr.AppendFunc(&err, func() error {
			if err := os.RemoveAll(t.workDir); !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		})
	}
	return err
}
