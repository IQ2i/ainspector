FROM golang:1.25-bookworm AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# tree-sitter needs cgo
RUN CGO_ENABLED=1 go build -ldflags="-s -w -X github.com/iq2i/ainspector/cmd.version=${VERSION}" -o /out/ainspector .

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 ainspector
COPY --from=build /out/ainspector /usr/local/bin/ainspector
USER ainspector
WORKDIR /home/ainspector
