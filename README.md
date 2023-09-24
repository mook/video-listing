# video-listing

This will be a HTTP server to manage a collection of videos with support for
playing items via Chromecast (with transcoding), as well as marking things as
watched.

## Plans

- golang http server
- [Chromecast web sender](https://developers.google.com/cast/docs/web_sender)
- Use ffmpeg to package videos for DASH
  - Previous plans were gstreamer, but the DASH & HLS plugins didn't like me.
- Serve the page on HTTPS via GitHub Pages, talking to the golang HTTP server
  with JSON (to get around requirements for HTTPS)
