# ---------- frontend ----------
FROM node:22-alpine AS web
WORKDIR /src/web/app
COPY web/app/package.json web/app/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY web/app .
RUN npm run build   # emits ../dist

# ---------- backend ----------
FROM golang:1.24-alpine AS build
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=2.0.0
ARG STATIC=0
RUN LDFLAGS="-s -w -X musicseer/internal/server.Version=${VERSION}"; \
    if [ "$STATIC" = "1" ]; then LDFLAGS="$LDFLAGS -linkmode external -extldflags -static"; fi; \
    CGO_ENABLED=1 go build -trimpath -ldflags "$LDFLAGS" -o /musicseer ./cmd/musicseer

# ---------- runtime ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S musicseer && adduser -S -G musicseer musicseer
COPY --from=build /musicseer /usr/local/bin/musicseer
USER musicseer
ENV MUSICSEER_DATA_DIR=/data \
    MUSICSEER_PORT=8688
VOLUME /data
EXPOSE 8688
HEALTHCHECK --interval=30s --timeout=5s \
  CMD wget -qO- http://127.0.0.1:8688/api/status >/dev/null || exit 1
ENTRYPOINT ["musicseer"]
