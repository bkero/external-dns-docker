# Stage 1: build
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /out/external-dns-docker ./cmd/external-dns-docker

# Stage 2: runtime
# The daemon must read the Docker socket, so it runs as root inside the
# container. Mount the socket read-only (:ro) to limit blast radius.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /out/external-dns-docker /usr/local/bin/external-dns-docker

ENTRYPOINT ["/usr/local/bin/external-dns-docker"]
