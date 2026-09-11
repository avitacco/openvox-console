# OpenVox Console

A Puppet Enterprise console equivalent for the OpenVox project. See
`architecture-summary.md` and `phased-build-plan.md` for the full design
and build order, and `openspec/specs/` for the current behavior
specification.

**Setting this up on real hosts?** See `SETUP.md` - a step-by-step guide
covering openvoxserver, openvoxdb, Postgres, the console, and node
enrolment, with both container and package instructions for each. The
section below is the throwaway local dev stack instead, and
`operations.md` covers running a deployment once it exists.

## Local development

The console binary is built and run directly on the host - only its
dependencies (Postgres, openvoxserver/openvoxdb, and the postgres-replication
failover fixture) run in containers, all started by a single `docker compose
up`. Config is read from `.env` (see `.env` itself, or
`internal/runtime/config.go` for what's actually read).

```sh
make up            # start every dependency service (Postgres, openvox stack, replication fixture)
make build          # build bin/console
make run             # build + run against the dependency services
make test            # run the test suite (needs `make up` for Postgres-backed tests)
```

`make openvox-up`/`openvox-down` and `make postgres-replication-up`/`-down`
restart or stop just those subsets without touching the rest of the stack.

### Working with openvoxdb (Phase 1+)

Inventory and reporting need a real openvoxdb to query. `make up` already
started it; seed it with real data and generate a client certificate for the
console:

```sh
make openvox-test     # run a real openvox-agent to generate a real node/report
```

The console authenticates to openvoxdb with a client certificate issued by
the same OpenVox CA (`openvoxserver`), matching how any other OpenVox
client would. Generate one:

```sh
docker exec enterprise-console-openvoxserver-1 \
  puppetserver ca generate --certname console

mkdir -p certs
docker cp enterprise-console-openvoxserver-1:/etc/puppetlabs/puppet/ssl/certs/console.pem certs/console-cert.pem
docker cp enterprise-console-openvoxserver-1:/etc/puppetlabs/puppet/ssl/private_keys/console.pem certs/console-key.pem
docker cp enterprise-console-openvoxserver-1:/etc/puppetlabs/puppet/ssl/certs/ca.pem certs/ca.pem
```

`certs/` is gitignored - these are local dev credentials, not committed.
Point `.env`'s `CONSOLE_OPENVOXDB_CERT_FILE` / `_KEY_FILE` / `_CA_FILE` at
them (absolute paths - see the comment in `.env`, since `go test` changes
the working directory per package, relative paths won't resolve
consistently). If you ever wipe the openvox stack's volumes (`docker
compose down -v`), the CA is reset too, so you'll need to delete the
old `console.pem` key/cert files inside the `openvoxserver` container (or
just regenerate after a fresh `make openvox-up`) and repeat the steps
above.

Then:

```sh
make run
```

and visit `http://localhost:8080/` for the node inventory, or
`http://localhost:8080/api/v1/nodes` directly.

### Node classification (Phase 2+)

openvoxserver uses this console as its External Node Classifier (ENC) via
Puppet's standard `node_terminus = exec` mechanism: `cmd/enc-bridge` is a
small binary that, given a certname, calls the console's
`GET /api/v1/enc/{certname}` and writes the YAML document Puppet's
exec-terminus expects.

`docker-compose.yml` already wires this up:
`openvoxserver-init/10-configure-enc.sh` (mounted at
`/container-custom-entrypoint.d/`) sets `node_terminus`/`external_nodes` in
`puppet.conf` on every boot, and `bin/enc-bridge` is bind-mounted into the
container at `/usr/local/bin/enc-bridge`. Since `enc-bridge` runs inside
that container but needs to reach the console running on the host, it's
pointed at `http://host.docker.internal:8080` (`ENC_BRIDGE_URL`); this
needs `make build` to have produced `bin/enc-bridge` *before*
`make openvox-up`/`--force-recreate` runs, and the console itself needs to
be reachable at `localhost:8080` (`make run`) for classification requests
to succeed.

To pick up a puppet.conf change after the console is already running:

```sh
docker compose up -d --force-recreate openvoxserver
```

Then create a node group via `http://localhost:8080/groups.html` (or the
`/api/v1/groups` API) matching a real fact - e.g. `kernel = Linux` - and
run `make openvox-test` again: the resulting catalog will include
resources from any class that group declares.

### Authentication and authorization (Phase 3+)

Every API endpoint requires a bearer token carrying the right permission
(e.g. `nodes:read`, `classifier:write`, `enc:read`). Tokens are signed
ES256 JWTs; generate a signing key once with `make rbac-keys` and point
`.env`'s `CONSOLE_RBAC_SIGNING_KEY_FILE` at it (see the comment in `.env`).

**First admin user.** On startup, if the `users` table is empty and
`CONSOLE_BOOTSTRAP_ADMIN_USERNAME` / `CONSOLE_BOOTSTRAP_ADMIN_PASSWORD` are
set in `.env`, the console creates that user with every permission
(`rbac:admin` included) and logs it. This is a no-op once any user
exists, so it's safe to leave those two vars set permanently - change the
password via the UI or `PUT /api/v1/users/{id}` after first login, or
just clear the vars once you no longer need auto-bootstrap. Log in at
`http://localhost:8080/login.html`, or via the API:

```sh
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"change-me-immediately"}'
```

This returns an `accessToken` (15 min TTL) and `refreshToken` (24h TTL,
rotated on use via `POST /api/v1/auth/refresh`). The frontend handles
this automatically; API clients should attach `Authorization: Bearer
<accessToken>`.

**Service tokens** are long-lived, admin-issued tokens for machine
clients (no login involved) with a fixed permission set chosen at
creation. `enc-bridge` (see above) needs one scoped to `enc:read` to call
the console's ENC endpoint from inside the `openvoxserver` container:

```sh
curl -X POST http://localhost:8080/api/v1/service-tokens \
  -H "Authorization: Bearer <admin access token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"enc-bridge","permissions":["enc:read"]}'
```

The response's `token` field is shown once - copy it into `.env`'s
`ENC_BRIDGE_TOKEN` (see the comment there), then recreate the
`openvoxserver` container so it picks up the new environment variable:

```sh
docker compose up -d --force-recreate openvoxserver
```

Any token (user or service) can be revoked by its `jti` via
`POST /api/v1/auth/logout` (for the caller's own tokens) or an admin
deleting a service token (`DELETE /api/v1/service-tokens/{id}`);
revocation propagates to every running console instance over NATS and
takes effect immediately, without a restart.

### OIDC login (Phase 3+)

The console supports OIDC (OpenID Connect) login as an alternative to
username/password - the login page offers both, and OIDC never replaces
local login or service tokens. It's entirely optional: leaving
`CONSOLE_OIDC_ISSUER` unset (the default) keeps OIDC inactive.

**Configuration** (all `CONSOLE_OIDC_*`, see `.env`'s comments and
`internal/runtime/config.go`):

- `CONSOLE_OIDC_ISSUER`, `CONSOLE_OIDC_CLIENT_ID`,
  `CONSOLE_OIDC_CLIENT_SECRET`, `CONSOLE_OIDC_REDIRECT_URL` - standard
  OIDC client registration values from your identity provider.
- `CONSOLE_OIDC_SCOPES` (default `openid profile email groups`).
- `CONSOLE_OIDC_USERNAME_CLAIM` (default `email`) - which ID token claim
  seeds the local, human-readable username on first login.
- `CONSOLE_OIDC_ROLE_CLAIM` (default `groups`) and
  `CONSOLE_OIDC_ROLE_MAPPING` (a JSON object, claim-value -> local role
  name, e.g. `{"platform-admins":"admin"}`) - on every OIDC login, the
  console assigns/unassigns roles to match the user's *current* claim
  values, without ever touching a role an administrator assigned by hand
  (see `design.md` in the `oidc-authentication` change for the full
  reconciliation rule).

**Your identity provider must include the username and role-mapping
claims directly in the ID token**, not only via its userinfo endpoint -
some providers withhold non-essential claims from the ID token by default
whenever an access token is also issued, and need an explicit setting to
include them anyway.

**Local dev / testing** points at a real, self-hosted OIDC provider (not a
mock) via `.env`'s `CONSOLE_OIDC_*` values - see `.env`'s comments for what
each one expects from your provider's client registration.

Then:

```sh
make run
```

and open `http://localhost:8080/login.html` - a "Log in with SSO" option
appears automatically once `GET /api/v1/auth/methods` reports OIDC is
configured.

### Code deployment (Phase 5+)

The console deploys Puppet code (Puppetfile + manifests) from a control
repo into the live environment directory `openvoxserver` reads from
(`openvox-code/environments/`), by shelling out to a real `g10k` binary
- **not** vendored in-process (see `design.md` in the
`phase-5-code-manager` change for why: `g10k` is `package main` with no
importable API, so this deliberately deviates from
`architecture-summary.md`'s original "in-process" plan).

**One-time local dev setup:**

```sh
make g10k-install          # builds a real g10k binary into bin/
make control-repo-fixture  # creates a real local bare git control repo at .dev/control-repo.git
```

`control-repo-fixture` is a genuine git repository (not a mock) with a
real Puppetfile (`puppetlabs/stdlib`) and a `code_manager_test_marker`
class, on a `production` branch - point `.env`'s
`CONSOLE_CONTROL_REPO_URL` at it
(`file:///path/to/enterprise-console/.dev/control-repo.git`) to deploy
from it locally. Re-run `make control-repo-fixture` any time to reset it
back to that known-good starting state.

Then:

```sh
make run
```

and trigger a deploy via `POST /api/v1/code-deploys` (needs
`code:deploy`) or the console's Deploys page.
