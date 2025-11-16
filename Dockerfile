# --- Build stage ---
# Match project toolchain (go 1.25)
FROM golang:1.25 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/jf-server ./cmd/server

# --- Runtime (distroless) ---
FROM gcr.io/distroless/base-debian12
WORKDIR /srv
COPY --from=build /out/jf-server /usr/local/bin/jf-server
# Optional: copy default config; can be overridden by mounting
COPY config.yaml /srv/config.yaml
VOLUME /srv/data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jf-server"]

