FROM alpine:edge AS builder
WORKDIR /go/src/video-listing
RUN --mount=type=cache,target=/var/cache/apk \
    apk add -U go gstreamer-dev
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download && go mod verify

ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
COPY . .
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go build -o /go/video-listing .

FROM alpine:edge
RUN --mount=type=cache,target=/var/cache/apk \
    apk add -U gst-libav gst-plugins-bad
COPY --from=builder /go/video-listing /usr/local/bin/video-listing
VOLUME [ "/config", "/cache", "/media" ]
ENTRYPOINT [ "/usr/local/bin/video-listing" ]
