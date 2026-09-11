package runtime

// Version is the console's build version, set via -ldflags at build time
// (see the Makefile's build target, which injects `git describe --tags
// --always --dirty`). Left at "dev" for invocations that skip that flag,
// e.g. a plain `go run ./cmd/console` or `go build` during local
// development.
var Version = "dev"
