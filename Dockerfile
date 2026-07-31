# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/rss-reader .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S rssreader \
    && adduser -S -G rssreader rssreader \
    && mkdir -p /app/data \
    && chown -R rssreader:rssreader /app

WORKDIR /app
COPY --from=build --chown=rssreader:rssreader /out/rss-reader /app/rss-reader
COPY --chown=rssreader:rssreader config.example.json /app/config.example.json
COPY --chown=rssreader:rssreader --chmod=755 docker-entrypoint.sh /app/docker-entrypoint.sh

ENV RSS_READER_HOST=0.0.0.0 \
    RSS_READER_PORT=8080 \
    RSS_READER_CONFIG=/app/data/config.json \
    RSS_READER_ARCHIVES=/app/data/archives.txt

USER rssreader
VOLUME ["/app/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O - "http://127.0.0.1:${RSS_READER_PORT}/healthz" >/dev/null || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
