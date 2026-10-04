FROM golang:1.26-alpine AS build

RUN apk update && apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev

WORKDIR /build

# Copiar apenas arquivos de dependências primeiro para cachear o download
COPY go.mod go.sum ./

# whatsmeow agora vem do proxy oficial (go.mau.fi/whatsmeow, sem replace local) —
# não há mais submódulo whatsmeow-lib para copiar.
RUN go mod download

# Copiar o restante do código
COPY . .

ARG VERSION=dev
# -trimpath and -s -w: no build paths in the binary, no symbol/debug tables (a smaller image).
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o server ./cmd/whatygo

# The runtime stage uses the same Alpine release as the build stage: the binary links to the
# jpeg and webp libraries of the system.
FROM alpine:3.24 AS final

# poppler-utils provides pdftoppm, used to rasterize PDF page 1 for /send/media document thumbnails
# su-exec drops root after the entrypoint has fixed the ownership of the data volumes.
RUN apk update && apk add --no-cache tzdata ffmpeg libjpeg-turbo libwebp poppler-utils su-exec \
    && addgroup -S -g 10001 whatygo \
    && adduser -S -u 10001 -G whatygo -h /app whatygo

WORKDIR /app

COPY --from=build /build/server .
COPY --from=build /build/manager/dist ./manager/dist
COPY --from=build /build/VERSION ./VERSION
COPY docker/entrypoint.sh /entrypoint.sh

# dbdata (SQLite auth DB) and logs are written at run time.
RUN mkdir -p /app/dbdata /app/logs && chown -R whatygo:whatygo /app && chmod +x /entrypoint.sh

ENV TZ=America/Sao_Paulo
ENV SERVER_PORT=8080

# Liveness: the process is up and serving HTTP (/server/ok does not look at the databases).
HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${SERVER_PORT}/server/ok" || exit 1

# The entrypoint starts as root only to hand the data volumes to the whatygo user (volumes
# created by an earlier image belong to root), then runs the server as that user.
ENTRYPOINT ["/entrypoint.sh"]
CMD ["/app/server"]
