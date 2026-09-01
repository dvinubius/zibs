# Build the SQLite CGO driver on the same Debian family used at runtime.
FROM golang:1.25.0-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/zibs .

FROM debian:bookworm-slim

RUN groupadd --system zibs \
    && useradd --system --gid zibs --home-dir /data --create-home zibs

WORKDIR /data

COPY --from=build --chown=zibs:zibs /out/zibs /usr/local/bin/zibs

USER zibs:zibs

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/zibs"]
