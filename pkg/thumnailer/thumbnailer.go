package thumnailer

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
	"github.com/sirupsen/logrus"
)

// CreateThumbnail creates a JPEG thumbnail from the given path.
func CreateThumbnail(ctx context.Context, path string) ([]byte, error) {
	logrus.WithField("path", path).Trace("Creating thumbnail...")

	sample, err := generateSample(path)
	if err != nil {
		return nil, fmt.Errorf("failed to sample %s: %w", path, err)
	}

	// Scale the image while keeping the aspect ratio
	structure := sample.GetCaps().GetStructureAt(0)
	width, err := structure.GetValue("width")
	if err != nil {
		return nil, fmt.Errorf("failed to get thumbnail width: %w", err)
	}
	height, err := structure.GetValue("height")
	if err != nil {
		return nil, fmt.Errorf("failed to get thumbnail height: %w", err)
	}
	desiredHeight := int(int64(height.(int)) * 320 / int64(width.(int)))
	sample, err = scaleSample(ctx, sample, 320, desiredHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to scale sample: %w", err)
	}

	return sample.GetBuffer().Bytes(), nil
}

// generateSample renders a sample image from the given video file
func generateSample(path string) (*gst.Sample, error) {
	// Construct a pipeline to decode the file
	u := &url.URL{Scheme: "file", Path: path}
	pipeline, err := gst.NewPipelineFromString(fmt.Sprintf(`
		uridecodebin uri=%s ! videoconvertscale ! appsink name=sink
	`, u.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to make pipeline: %w", err)
	}
	// Get the sink element from the pipeline
	sinkElement, err := pipeline.GetElementByName("sink")
	if err != nil {
		return nil, fmt.Errorf("failed to get sink: %w", err)
	}
	sink := app.SinkFromElement(sinkElement)

	// Pause the pipeline (preroll)
	if err = pipeline.SetState(gst.StatePaused); err != nil {
		return nil, fmt.Errorf("failed to pause pipeline: %w", err)
	}
	stateResult, _ := pipeline.GetState(gst.StatePaused, gst.ClockTime(5*time.Second))
	if stateResult == gst.StateChangeFailure {
		return nil, fmt.Errorf("failed to preroll: %s", &stateResult)
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
		return nil, fmt.Errorf("failed to get sample for %s", path)
	}

	return sample, nil
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
