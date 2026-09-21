## Context

See proposal.md - Why. The constraint that drives every decision here:
`g10k` is `package main` with no importable API, so the console runs it
as a subprocess (see the phase-5-code-manager design), and `g10k` itself
does not speak the git protocol - it execs a real `git` for every clone,
fetch and archive, and splits command strings itself rather than going
through a shell.

So the image needs three things, not one: g10k, git, and git's
transports. It does not need a shell.

## Goals / Non-Goals

**Goals:**

- A published image where configuring a control repo is the only step
  between an operator and a working deploy.
- A reproducible image: the g10k revision pinnable, its version
  reportable from the running binary.

**Non-Goals:**

- Vendoring g10k. That was settled in phase-5-code-manager and nothing
  here revisits it.
- Making the image a general-purpose Puppet toolbox. It carries what a
  deploy needs and nothing else.

## Decisions

### Not distroless, and the reason is git rather than the console

The console binary is `CGO_ENABLED=0` static and would run happily on
distroless. g10k would too. `git` would not - and an image carrying
g10k without git fails every deploy at runtime with the binary present
and apparently working, which is a worse failure than not shipping g10k
at all.

### Alpine over debian-slim, decided by measurement

Debian's `git` hard-depends on perl. Measured rather than assumed:
debian-slim 325MB, Alpine 165MB, against 129MB for the distroless image
being replaced. Alpine costs 36MB over distroless and makes code
deployment work at all.

musl does not enter into it: both binaries are static, so nothing in the
final stage links against the C library. That is worth stating because
"Alpine" usually raises the musl question, and here it is simply not a
question.

### g10k is built from source in the image, not fetched

Built from a shallow clone rather than `go install ...@latest`, so the
build does not depend on upstream having tagged a release, and
cross-compiled for the target platform alongside the console rather than
under emulation.

A build argument pins the revision; empty - the default - tracks the
upstream default branch, matching what the local make target does. The
build stamps g10k's own version flags, because an unstamped binary
reports an empty version, and that version is the first thing an
operator reaches for when a deploy misbehaves.

### Providing the tool is not enabling the feature

The image presets the binary path, which is otherwise the one setting an
operator cannot supply from outside the image. Code deployment still
requires a control repo to be configured, so the preset turns nothing on
by itself - it removes a step that only the image can take.

## Risks / Trade-offs

**The image tracks g10k's default branch by default** → An upstream
change lands in the next image build without review. Mitigated by the
pinning build argument; a release build should use it. Left as the
default because it matches the local make target, and diverging would be
its own surprise.

**36MB over distroless** → Accepted deliberately, measured rather than
estimated, and the alternative is a feature that cannot work.

**A larger base has more in it to patch** → Real, and the reason the
image installs only `ca-certificates`, `git` and `openssh-client`
instead of a convenience bundle.

## Migration Plan

Additive. An existing deployment that mounts its own g10k binary and
sets the path explicitly keeps working - an explicit setting overrides
the image's default.

Rollback is a previous image tag; nothing persists and no schema is
involved.
