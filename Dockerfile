FROM alpine:edge AS builder
WORKDIR /go/src/video-listing
RUN --mount=type=cache,target=/var/cache/apk \
    apk add -U ca-certificates git go
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download && go mod verify

ENV CGO_ENABLED=0
COPY . .
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -v -o /go/video-listing .

FROM alpine:edge
RUN --mount=type=cache,target=/var/cache/apk \
    apk add -U ffmpeg
COPY --from=builder /etc/ssl/certs /etc/ssl/certs
COPY --from=builder /go/video-listing /video-listing
VOLUME [ "/media" ]
ENTRYPOINT [ "/video-listing" ]
ENV PORT=80
EXPOSE "80/tcp"
