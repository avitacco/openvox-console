## Why

Code deployment runs `g10k` as a subprocess, and `g10k` in turn execs a
real `git` for every clone, fetch and archive. The published console
image carried neither. An operator running the console in a container
had a binary that starts, serves, and reports code deployment as simply
unconfigured - with no way to configure it, because the tool it needs is
not in the image and cannot be installed into one that has no package
manager.

The documentation made this worse rather than better: the control-repo
runbook told operators the published image already contained `g10k`,
which was false.

## What Changes

- **The console image ships the code deployment toolchain**: `g10k`
  itself, the `git` it shells out to, and the transports git needs for
  HTTPS and SSH remotes.

- **The image presets the g10k binary location**, so an operator
  configures a control repo and nothing else. Code deployment still
  stays inactive until a control repo is configured, so the image
  enables nothing by itself.

- **The g10k revision is pinnable at build time** for a reproducible
  image, defaulting to the upstream default branch.

- Corrects the control-repo runbook, which claimed the image already
  contained `g10k`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `code-manager`: adds a requirement that a deployment artifact of the
  console provides everything a deploy needs, rather than leaving code
  deployment unusable in the environment the console is most often run
  in.

## Impact

- `Dockerfile`: builds `g10k` for the target platform, installs `git`
  and its transports, presets the binary path.
- `Makefile`: `g10k-install` was building the wrong package path and
  never worked; fixed, and both paths now stamp g10k's version.
- `README.md`, and the control-repo runbook's false claim.
