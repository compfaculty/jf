FROM golang:1.25-alpine AS build
RUN apk add --no-cache build-base
ENV CGO_ENABLED=1
WORKDIR /src
COPY . .
RUN go build -o /out/server ./cmd/server

FROM alpine:latest
RUN apk add --no-cache sqlite
COPY --from=build /out/server /server
CMD ["/server"]
