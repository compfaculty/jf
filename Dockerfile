# --- Build stage ---
FROM golang:1.22 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /out/jf ./cmd/jf

# --- Runtime (distroless) ---
FROM gcr.io/distroless/base-debian12
WORKDIR /srv
COPY --from=build /out/jf /usr/local/bin/jf
COPY config/ ./config/
COPY web/ ./web/
VOLUME /srv/data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jf"]

