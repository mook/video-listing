# video-listing

This will be a HTTP server to manage a collection of videos with support for
playing items via Chromecast (with transcoding), as well as marking things as
watched.

## Plans

- golang http server
- [Chromecast web sender](https://developers.google.com/cast/docs/web_sender)
- [gstreamer hlssink2](https://gstreamer.freedesktop.org/documentation/hls/hlssink2.html)
- [go-gst](https://pkg.go.dev/github.com/go-gst/go-gst)
