# video-listing

This will be a HTTP server to manage a collection of videos with support for
playing items via Chromecast (with transcoding), as well as marking things as
watched.

## Plans

- golang http server
- [Chromecast web sender](https://developers.google.com/cast/docs/web_sender)
- [gstreamer hlssink2](https://gstreamer.freedesktop.org/documentation/hls/hlssink2.html)
- [go-gst](https://pkg.go.dev/github.com/go-gst/go-gst)

## Notes

### ffmpeg

screenshot:

```sh
ffmpeg -i in.mp4 -ss 1:0 -vframes 1 out.png
```

probe:

```sh
ffprobe -print_format json -v quiet -show_streams -show_format in.mp4
```

pack:

```sh
ffmpeg -i in.webm -f dash -seg_duration 2 -single_file 1 -codec:v copy out.mpd
```
