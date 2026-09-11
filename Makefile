BINARY     := bin/console
ENC_BRIDGE := bin/enc-bridge
IMAGE      := enterprise-console:latest

# Falls back to "dev" outside a git checkout (e.g. no commits yet) or
# without git installed - see internal/runtime.Version's own doc comment.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

-include .env
export

.PHONY: build frontend agent-binaries agent-packages docker-build up down openvox-up openvox-down openvox-test g10k-install control-repo-fixture rbac-keys rbac-rotate-key postgres-replication-up postgres-replication-down postgres-replication-failover run test clean

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
	# voxpupuli/g10k's go.mod still declares its module path as
	# github.com/xorpaul/g10k (the upstream it's forked from), so `go
	# install github.com/voxpupuli/g10k@latest` fails on a module-path
	# mismatch. Cloning and building locally sidesteps that - it doesn't
	# depend on the module's self-declared import path.
	tmpdir=$$(mktemp -d); \
	git clone --depth 1 https://github.com/voxpupuli/g10k.git "$$tmpdir" && \
	(cd "$$tmpdir" && go build -o $(CURDIR)/bin/g10k .); \
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

run: build ## Run the binary locally against the dependency services (make up first); reads config from .env
	./$(BINARY)

test: ## Run the test suite (Postgres-backed tests need `make up` + CONSOLE_TEST_POSTGRES_DSN in .env)
	go test ./...

clean:
	rm -rf bin
