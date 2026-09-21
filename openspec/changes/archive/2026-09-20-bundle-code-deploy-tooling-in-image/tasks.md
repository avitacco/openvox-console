## 1. Image

- [x] 1.1 Build g10k for the target platform in the image build and install it; verify `g10k -version` runs in the built image
- [x] 1.2 Install git and the CA/SSH material its transports need; verify `git --version` and a real `ls-remote` against an HTTPS remote from inside the image
- [x] 1.3 Preset the g10k binary path in the image environment; verify it is set and that code deployment still reports unconfigured without a control repo
- [x] 1.4 Make the g10k revision pinnable at build time, defaulting to the upstream default branch; verify a pinned build
- [x] 1.5 Keep the image size defensible; verify by measuring against the alternatives rather than assuming

## 2. Local build path

- [x] 2.1 Fix `make g10k-install`, which built the wrong package path and could never have worked; verify it produces a runnable binary
- [x] 2.2 Stamp g10k's version in both the image and the make target; verify `-version` reports a real value from each

## 3. Documentation

- [x] 3.1 Correct the control-repo runbook's claim that the published image already contained g10k, and state what an older image does instead
- [x] 3.2 Document the bundled toolchain and the pinning build argument in `README.md`
