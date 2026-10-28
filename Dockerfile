FROM golang:1.25-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# tree-sitter needs cgo
RUN CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/iq2i/ainspector/cmd.version=${VERSION}" -o /out/ainspector .

FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 ainspector
COPY --from=build /out/ainspector /usr/local/bin/ainspector
USER ainspector
WORKDIR /home/ainspector
