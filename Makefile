BINARY     := bin/console
ENC_BRIDGE := bin/enc-bridge
IMAGE      := enterprise-console:latest

# Falls back to "dev" outside a git checkout (e.g. no commits yet) or
# without git installed - see internal/runtime.Version's own doc comment.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

-include .env
export

.PHONY: build frontend agent-binaries agent-packages docker-build up down openvox-up openvox-down openvox-test g10k-install control-repo-fixture code-sources-fixture rbac-keys rbac-rotate-key postgres-replication-up postgres-replication-down postgres-replication-failover run test clean demo-seed screenshots-up screenshots-down marketing marketing-build marketing-serve marketing-screenshots marketing-a11y

# Compose invocation for a screenshot capture run. All three files, in
# this order: naming any -f turns off compose's automatic loading of
# docker-compose.override.yml, and without the override openvoxdb comes
# back with no published port and no "localhost" alt name - healthy, and
# unreachable from the host where the seed and capture tools run.
SCREENSHOT_COMPOSE := docker compose -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.screenshots.yml

build: ## Build the console binary and enc-bridge for your local OS/arch
	go build -ldflags "-X github.com/voxpupuli/enterprise-console/internal/runtime.Version=$(VERSION)" -o $(BINARY) ./cmd/console
	# enc-bridge runs inside the openvoxserver container, not on this host,
	# so it's built statically (CGO_ENABLED=0) to avoid any glibc mismatch
	# with that container's base image.
	CGO_ENABLED=0 go build -o $(ENC_BRIDGE) ./cmd/enc-bridge

frontend: ## Rebuild the embedded frontend bundle from frontend/src
	cd frontend && ./build.sh

agent-binaries: ## Cross-compile node-agent-client for internal/agentdist to embed (see design.md - console build time, not on-demand)
	mkdir -p internal/agentdist/bin
	GOOS=linux GOARCH=amd64 go build -o internal/agentdist/bin/node-agent-client-linux-amd64 ./cmd/node-agent-client
	GOOS=linux GOARCH=arm64 go build -o internal/agentdist/bin/node-agent-client-linux-arm64 ./cmd/node-agent-client
	# Windows is amd64-only: OpenVox publishes no Windows ARM64
	# openvoxagent package for a node-agent build to pair with (see
	# design.md in add-multi-platform-agent-install).
	GOOS=windows GOARCH=amd64 go build -o internal/agentdist/bin/node-agent-client-windows-amd64 ./cmd/node-agent-client
	GOOS=darwin GOARCH=amd64 go build -o internal/agentdist/bin/node-agent-client-darwin-amd64 ./cmd/node-agent-client
	GOOS=darwin GOARCH=arm64 go build -o internal/agentdist/bin/node-agent-client-darwin-arm64 ./cmd/node-agent-client

agent-packages: agent-binaries ## Build node-agent-client .deb/.rpm packages for internal/agentdist to embed and serve (see design.md in add-native-agent-packaging - Linux only, see that design.md for why Windows/macOS aren't packaged natively)
	go run ./cmd/build-agent-packages "$(VERSION)"

docker-build: ## Build a container image of the console (not part of the normal dev workflow)
	# Self-contained: the Dockerfile generates the frontend bundle and the
	# embedded node-agent-client binaries/packages itself, so this works
	# from a clean checkout with no prior make targets.
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

up: ## Start every service the application depends on (Postgres, openvox stack, postgres-replication fixture)
	docker compose up -d --wait

down: ## Stop all dependency services
	docker compose down

openvox-up: ## Restart just the openvox stack (openvoxserver + openvoxdb + their own Postgres) without touching the rest
	docker compose up -d --wait openvoxserver openvoxdb openvoxdb-postgres

openvox-down: ## Stop just the openvox stack, leaving everything else running
	docker compose down openvoxserver openvoxdb openvoxdb-postgres

openvox-test: ## Run a real openvox-agent against openvoxserver to generate real data in openvoxdb
	docker compose --profile openvox-test run --rm openvox-testing-agent agent -t --server openvoxserver --waitforcert 0

g10k-install: ## Build a real g10k binary into bin/ (one-time local dev setup)
	# Cloned rather than `go install ...@latest` so this doesn't depend
	# on upstream having tagged a release. The main package is
	# ./cmd/g10k, not the repository root - building "." fails with
	# "no Go files in ...". The container image builds it the same way,
	# see Dockerfile.
	# The -X flags are g10k's own: without them `g10k -version` reports an
	# empty version.
	tmpdir=$$(mktemp -d); \
	git clone --depth 1 https://github.com/voxpupuli/g10k.git "$$tmpdir" && \
	(cd "$$tmpdir" && go build \
		-ldflags "-X main.buildversion=$$(git describe --tags --always 2>/dev/null || echo dev) -X main.buildtime=$$(date -u '+%Y-%m-%d_%H:%M:%S')" \
		-o $(CURDIR)/bin/g10k ./cmd/g10k); \
	rm -rf "$$tmpdir"

control-repo-fixture: ## Create a real local bare git control repo for code-manager dev/testing (one-time setup)
	rm -rf .dev/control-repo.git
	mkdir -p .dev
	git init --bare .dev/control-repo.git
	tmpdir=$$(mktemp -d); \
	git clone .dev/control-repo.git "$$tmpdir" && \
	mkdir -p "$$tmpdir/manifests" && \
	printf 'forge "https://forgeapi.puppet.com"\nmod '"'"'puppetlabs/stdlib'"'"', '"'"'9.6.0'"'"'\n' > "$$tmpdir/Puppetfile" && \
	printf 'node default {\n  class { '"'"'code_manager_test_marker'"'"': }\n}\n\nclass code_manager_test_marker {\n  file { '"'"'/tmp/enterprise-console-code-manager-test-marker'"'"':\n    ensure  => file,\n    content => "deployed by phase-5-code-manager\\n",\n  }\n}\n' > "$$tmpdir/manifests/site.pp" && \
	printf 'modulepath = modules:site\n' > "$$tmpdir/environment.conf" && \
	(cd "$$tmpdir" && git -c user.email=dev@localhost -c user.name=dev -c commit.gpgsign=false add -A && git -c user.email=dev@localhost -c user.name=dev -c commit.gpgsign=false commit -q -m "initial control repo fixture" && git branch -M production && git push -q origin production); \
	rm -rf "$$tmpdir"
	git -C .dev/control-repo.git symbolic-ref HEAD refs/heads/production

code-sources-fixture: control-repo-fixture ## Add a second control repo + a code-sources.yaml, for multi-source code deployment dev/testing
	# Deliberately a SECOND repo with the same `production` branch name:
	# that collision is the thing prefixes exist to resolve, so a
	# fixture without it would not exercise the feature at all.
	rm -rf .dev/team-a-control.git
	git init --bare -q .dev/team-a-control.git
	tmpdir=$$(mktemp -d); \
	git clone -q .dev/team-a-control.git "$$tmpdir" && \
	mkdir -p "$$tmpdir/manifests" && \
	printf 'node default {\n  class { '"'"'team_a_marker'"'"': }\n}\n\nclass team_a_marker {\n  file { '"'"'/tmp/enterprise-console-team-a-marker'"'"':\n    ensure  => file,\n    content => "deployed from the team_a control repo\\n",\n  }\n}\n' > "$$tmpdir/manifests/site.pp" && \
	printf 'modulepath = modules:site\n' > "$$tmpdir/environment.conf" && \
	(cd "$$tmpdir" && git -c user.email=dev@localhost -c user.name=dev -c commit.gpgsign=false add -A && git -c user.email=dev@localhost -c user.name=dev -c commit.gpgsign=false commit -q -m "team_a control repo fixture" && git branch -M production && git push -q origin production); \
	rm -rf "$$tmpdir"
	git -C .dev/team-a-control.git symbolic-ref HEAD refs/heads/production
	printf 'sources:\n  control:\n    remote: file://$(CURDIR)/.dev/control-repo.git\n    prefix: false\n  team_a:\n    remote: file://$(CURDIR)/.dev/team-a-control.git\n    prefix: true\n' > .dev/code-sources.yaml
	@echo
	@echo "Wrote .dev/code-sources.yaml. To use it, set in .env:"
	@echo "  CONSOLE_CODE_SOURCES_PATH=$(CURDIR)/.dev/code-sources.yaml"
	@echo "and unset CONSOLE_CONTROL_REPO_URL - the two are mutually exclusive."
	@echo "Deploying production from each lands in environments/production"
	@echo "and environments/team_a_production respectively."

rbac-keys: ## Generate a local ES256 signing key for RBAC (one-time dev setup)
	mkdir -p certs
	openssl ecparam -genkey -name prime256v1 -noout -out certs/rbac-signing-key.pem

# Overridable: `make rbac-rotate-key KID=2026-09-01`. Defaults to today's
# date, which is a reasonable kid on its own for most rotation cadences.
KID ?= $(shell date -u +%Y-%m-%d)

rbac-rotate-key: ## Generate a new RBAC verification key for rotation, without touching the active signing key (see operations.md)
	mkdir -p certs/rbac-verification-keys
	openssl ecparam -genkey -name prime256v1 -noout -out certs/rbac-verification-keys/$(KID).pem
	@echo "Generated certs/rbac-verification-keys/$(KID).pem (kid=$(KID))."
	@echo "certs/rbac-signing-key.pem (the currently active signer) is untouched."
	@echo "See operations.md's key rotation runbook for the promote/retire sequence."

postgres-replication-up: ## Restart just the Postgres primary+standby+proxy failover fixture (phase-8-hardening, see operations.md)
	docker compose up -d --wait postgres-primary postgres-standby postgres-proxy

postgres-replication-down: ## Stop just the replication fixture, leaving everything else running
	docker compose down postgres-primary postgres-standby postgres-proxy

postgres-replication-failover: ## Promote the standby and repoint the proxy at it, without touching the console (see operations.md)
	docker exec $$(docker compose ps -q postgres-standby) psql -U console -d console -c "SELECT pg_promote();"
	POSTGRES_PROXY_TARGET=postgres-standby docker compose up -d postgres-proxy

demo-seed: ## Fill a LOCAL console with a fabricated demo fleet for screenshots/demos (see marketing/README.md; refuses any non-local target)
	# --reset first deletes the demo fleet from openvoxdb. Without it a
	# re-run is a no-op for reports: openvoxdb deduplicates them by
	# content hash and this seed is deterministic, so a fleet stored
	# under the wrong settings can never be corrected in place.
	go run ./cmd/demo-seed --reset --i-know-this-is-a-demo-console

marketing: ## Refresh every screenshot and rebuild the site into docs/ - starts what it needs, puts everything back afterwards (see marketing/refresh.sh)
	./marketing/refresh.sh

marketing-build: ## Rebuild the site from the committed screenshots only - quick, no containers (for editing text and guides)
	cd marketing && ./build.sh

marketing-a11y: ## Audit the built site against WCAG 2.2 AA (needs `make screenshots-up` for the browser)
	# Serves docs/ on a spare port, runs the audit against it, stops the
	# server whichever way the audit ends.
	@cd docs && python3 -m http.server 8778 >/dev/null 2>&1 & \
	server=$$!; \
	trap "kill $$server 2>/dev/null" EXIT; \
	sleep 1; \
	go run ./marketing/a11y --base http://host.docker.internal:8778 --best-practice

marketing-serve: ## Serve the built site locally so you can look at it before pushing
	@echo "Serving docs/ at http://localhost:8777/ - Ctrl-C to stop"
	@cd docs && python3 -m http.server 8777

marketing-screenshots: ## Refresh every marketing screenshot (needs `make screenshots-up` first)
	# Runs its own throwaway console against its own database, so your
	# development console's jobs, deploys and users stay out of the
	# published images - see marketing/screenshots.sh.
	./marketing/screenshots.sh

screenshots-up: ## Start the stack configured for screenshot capture (retention off, headless browser on)
	$(SCREENSHOT_COMPOSE) up -d --wait

screenshots-down: ## Stop the screenshot stack and return the services to their normal configuration
	$(SCREENSHOT_COMPOSE) down
	@echo
	@echo "openvoxdb's normal retention is back. It will garbage-collect the"
	@echo "demo fleet on its own schedule - reseed before capturing again."

run: build ## Run the binary locally against the dependency services (make up first); reads config from .env
	./$(BINARY)

test: ## Run the test suite (Postgres-backed tests need `make up` + CONSOLE_TEST_POSTGRES_DSN in .env)
	go test ./...

clean:
	rm -rf bin
