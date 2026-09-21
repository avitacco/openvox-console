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

# g10k, built for the target platform. internal/codemanager runs it as a
# subprocess rather than vendoring it (g10k is `package main` with no
# importable API), so an image without it cannot deploy code at all.
#
# Built from a clone rather than `go install ...@latest` so the build
# does not depend on upstream having tagged a release. The main package
# is ./cmd/g10k, not the repository root - building "." fails with
# "no Go files in ...".
#
# G10K_REF pins a tag or commit for a reproducible image; empty (the
# default) tracks the remote's default branch, matching the Makefile.
# The -X flags are g10k's own, and without them `g10k -version` prints an
# empty version - which is what an operator runs first when a deploy
# misbehaves. A --depth 1 clone fetches no tags, so `git describe` falls
# back to the bare commit sha; that still identifies the build exactly.
ARG G10K_REF=
RUN set -eux; \
    git clone --depth 1 https://github.com/voxpupuli/g10k.git /tmp/g10k; \
    cd /tmp/g10k; \
    if [ -n "$G10K_REF" ]; then \
        git fetch --depth 1 origin "$G10K_REF"; \
        git checkout --detach FETCH_HEAD; \
    fi; \
    g10k_version=$(git describe --tags --always 2>/dev/null || echo dev); \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build \
        -ldflags "-X main.buildversion=$g10k_version -X main.buildtime=$(date -u '+%Y-%m-%d_%H:%M:%S')" \
        -o /out/g10k ./cmd/g10k

# Not distroless/static, and deliberately so: g10k shells out to a real
# `git` for every clone, fetch and archive, so an image carrying g10k
# without git would fail every deploy at runtime with the binary present
# and apparently working. git in turn needs CA certificates for HTTPS
# remotes and ssh for SSH remotes. g10k itself needs no shell - it
# splits command strings and execs directly - so nothing here is for
# g10k's own benefit beyond git and git's transports.
#
# Alpine rather than debian-slim purely for size: Debian's git
# hard-depends on perl, which made the image 325MB against Alpine's
# 165MB (the distroless image it replaces was 129MB). Both the console
# and g10k are CGO_ENABLED=0 static binaries, so musl never enters into
# it - nothing in this stage links against the C library.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates git openssh-client

COPY --from=build /out/console /console
COPY --from=build /out/g10k /usr/local/bin/g10k

# Point the code manager at the bundled binary. Code deployment still
# stays disabled until CONSOLE_CONTROL_REPO_URL is also set, so this
# default turns nothing on by itself.
ENV CONSOLE_G10K_BIN_PATH=/usr/local/bin/g10k

# 8080: web UI and API. 8142: the NATS node transport nodes connect to.
EXPOSE 8080 8142
ENTRYPOINT ["/console"]
