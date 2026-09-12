# syntax=docker/dockerfile:1

# One build stage, running on the *build* host's architecture and
# cross-compiling for the target rather than running under emulation:
# everything produced here is either static assets or Go binaries, both
# of which cross-compile natively, so a multi-arch image costs no QEMU
# time.
# Pinned to the build platform explicitly. A bare
# `COPY --from=node:26-trixie-slim` resolves that image for the *target*
# platform, so building linux/arm64 would drop an arm64 node binary into
# this amd64 stage - which fails as "Syntax error: ) unexpected", the
# shell trying to interpret a binary it cannot execute.
FROM --platform=$BUILDPLATFORM node:26-trixie-slim AS nodesrc

FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
WORKDIR /src

# frontend/build.sh needs node and npm *alongside* Go - it copies the
# vendored voxblocks assets with npm, then generates the HTML pages with
# `go run ./gen`, writing both into internal/web/dist for go:embed to
# pick up. Taking node from its own official image keeps that version
# pinned without apt, and avoids a separate stage that could not run
# build.sh anyway (the script needs the Go module at the repo root).
COPY --from=nodesrc /usr/local/bin/node /usr/local/bin/node
COPY --from=nodesrc /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm

COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev

RUN ./frontend/build.sh

# node-agent-client's per-platform binaries and its .deb/.rpm packages
# are embedded into the console binary (internal/agentdist's go:embed)
# and served to nodes from it. They are generated, not committed, so
# they have to be produced here - without this a build from a clean
# checkout fails with "pattern all:bin: no matching files found".
# Filenames must match agentdist's own lookup:
# node-agent-client-<goos>-<goarch>, with no extension even for Windows.
RUN set -eux; \
    mkdir -p internal/agentdist/bin; \
    for target in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do \
        os="${target%/*}"; arch="${target#*/}"; \
        GOOS="$os" GOARCH="$arch" go build \
            -o "internal/agentdist/bin/node-agent-client-$os-$arch" \
            ./cmd/node-agent-client; \
    done; \
    go run ./cmd/build-agent-packages "$VERSION"

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build \
        -ldflags "-X github.com/voxpupuli/enterprise-console/internal/runtime.Version=$VERSION" \
        -o /out/console ./cmd/console

FROM gcr.io/distroless/static-debian13
COPY --from=build /out/console /console
# 8080: web UI and API. 8142: the NATS node transport nodes connect to.
EXPOSE 8080 8142
ENTRYPOINT ["/console"]
