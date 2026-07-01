# syntax=docker/dockerfile:1

# ---- build: static, CGO-free, cross-compiled to the target arch ----
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/tefnut ./cmd/tefnut

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S tefnut && adduser -S -G tefnut tefnut \
 && mkdir -p /comics /data && chown -R tefnut:tefnut /data
COPY --from=build /out/tefnut /usr/local/bin/tefnut
COPY deploy/config.yaml /etc/tefnut/config.yaml
EXPOSE 8086
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8086/healthz || exit 1
USER tefnut
ENTRYPOINT ["tefnut", "-config", "/etc/tefnut/config.yaml"]
