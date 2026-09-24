# OpenVox Console

A Puppet Enterprise console equivalent for the OpenVox project. See
`architecture-summary.md` and `phased-build-plan.md` for the full design
and build order, and `openspec/specs/` for the current behavior
specification.

**Setting this up on real hosts?** Follow the guides on the project site
(**Guides**, from any page) - step by step, from installing with
containers or packages, through adding nodes and scaling out, to
day-to-day use and runbooks. Their source is
[`marketing/guides/`](marketing/guides/), which reads fine here too. The
section below is the throwaway local dev stack instead, and
`operations.md` holds the engineering notes behind running a deployment.

**Container images:** two, built together for `linux/amd64` and
`linux/arm64` by `.github/workflows/ci.yml` on every push to `main`:

- `ghcr.io/avitacco/openvox-console` - the console itself.
- `ghcr.io/avitacco/openvox-console-server` - openvoxserver plus the
  `enc-bridge` binary and the `node_terminus = exec` wiring that make the
  console its node classifier (`Dockerfile.openvoxserver`). The stock
  openvoxserver image starts and looks healthy but classifies nothing.

`:main` follows that branch and every build also gets an immutable
`:sha-<commit>`; `:latest` and semver tags appear only when a `v*` tag is
pushed. Run matching tags for the two. `docker-compose.yml` pulls both -
see the **Install with containers** guide
([`marketing/guides/install/containers.md`](marketing/guides/install/containers.md)).

**Marketing site:** `marketing/` holds the source of the public site
describing what the console does, illustrated with screenshots captured
from a running instance rather than drawn, and the step-by-step guides
that are its user documentation. `docs/` is the built site,
committed and served by GitHub Pages straight from the branch. Neither is
part of the console binary.

It is built locally and pushed, not built in CI - so what is published is
something somebody has looked at.

```sh
make marketing              # screenshots + docs/, starting and stopping what it needs
make marketing-serve        # look at it before pushing
                            # then: git add docs && git commit && git push

make marketing-build        # just docs/, from the committed screenshots - quick
```

See `marketing/README.md` for what `make marketing` starts, and what it
puts back afterwards.

Refreshing runs its own throwaway console against its own database, so
your development console's jobs, deploys and users stay out of the
published images. See `marketing/README.md` for the details, including an
honest note on which parts of the demo data are fabricated.

**Languages:** the console's interface is translated with gettext
catalogues in `frontend/locales/`. English is the source language; 14
others are scaffolded, of which German is complete and the rest fall back
to English until translated. A user picks a language in Preferences;
without a choice the console follows the browser's.

```sh
cd frontend
go run ./i18n status            # completeness per language
go run ./i18n add <code>        # scaffold a new language
go run ./i18n extract && go run ./i18n merge   # after changing UI text
```

See `frontend/locales/README.md` for the translator-facing guide.

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

The container image needs neither: it builds `g10k` itself, installs it
at `/usr/local/bin/g10k` with the `git`/`ssh` it shells out to, and
presets `CONSOLE_G10K_BIN_PATH`. Code deployment still stays off until
`CONSOLE_CONTROL_REPO_URL` is set. `--build-arg G10K_REF=<tag-or-sha>`
pins the g10k revision; the default tracks its default branch.

**Multiple control repos.** `CONSOLE_CONTROL_REPO_URL` declares one. To
deploy from several, point `CONSOLE_CODE_SOURCES_PATH` at a YAML file
instead - the two are **mutually exclusive**, and setting both is a
startup error rather than a merge:

```yaml
sources:
  control:
    remote: git@github.com:org/control-repo.git
    prefix: false        # -> environments/production
  team_a:
    remote: git@github.com:org/team-a-control.git
    prefix: true         # -> environments/team_a_production
    private_key: /etc/openvox-console/keys/team_a
    webhook_secret_file: /run/secrets/team_a_webhook
```

`prefix` decides the Puppet environment name a source's branches
produce, and is how two repos can each carry a `production` branch. It
takes `true` (prefix with the source name), `false`/unset (no prefix),
or a literal string. Because two unprefixed sources would collide the
moment they shared a branch name, the console requires every source's
effective prefix to be distinct and refuses to start otherwise - in
practice, at most one source may be unprefixed.

Each source gets its own webhook endpoint,
`POST /api/v1/code-deploys/webhook/<source>`, verified against that
source's own `webhook_secret`. The unsuffixed
`/api/v1/code-deploys/webhook` continues to serve the default source
using `CONSOLE_CODE_WEBHOOK_SECRET`, so an existing git host needs no
change. Source names may contain only `[a-z0-9_]` - they become part of
a Puppet environment name.

`make code-sources-fixture` sets up a second local control repo and a
matching `.dev/code-sources.yaml` to develop against.

**The Code page** (`/code.html`, `code:read`) has two tabs.
**Repositories** lists every configured repo with its remote, prefix,
deployed environments, size and last successful deploy, plus two node
counts; clicking one opens **Deploy history** filtered to it.
`/deploys.html` redirects to that tab, so existing links still work.
The two counts are:

- **Assigned** - nodes the classifier sends to one of this repo's
  environments, using each node's *effective* classification, so a node
  matching several groups counts once against the environment it would
  actually be sent to.
- **Reporting** - nodes whose most recent Puppet run happened in one of
  them.

They are shown separately because the gap is the useful part. Assigned
with no reporting means code that has deployed but nothing has run yet;
reporting with no assigned means nodes running code no group directs
them to - usually an environment left behind by a removed source, or a
node with a hardcoded `environment` in its `puppet.conf`. The page names
these states rather than leaving you to compare two numbers.

Two things the numbers do *not* mean:

- **Size is content size, not disk usage.** It sums file sizes in the
  deployed tree. g10k hardlinks modules from a shared cache, so repos
  sharing a module version share those bytes on disk and the figures
  overlap. Deduplicating would make one repo's size change when an
  unrelated repo is deployed, which is worse.
- **Size and environments are recorded at deploy time**, so both read as
  "&mdash;" for deploys made before this shipped. They fill in on the
  next deploy.

Remotes are shown with any embedded credentials redacted
(`https://REDACTED@host/org/repo.git`), so a `code:read` user can see a
repo's host and path but not a token placed in its URL. Nodes using an
environment no configured repo has deployed are reported separately
rather than attributed to the unprefixed repo, which would be a guess.

Two things to expect when adding a source:

- A **prefixed source deploys successfully but changes nothing** until
  nodes are classified into its new environment name. Nothing
  reassigns them, so the first deploy looks like a no-op.
- **Removing a source from the config does not remove its
  environments.** Deploys stop; `environments/team_a_*` stays on disk
  and openvoxserver keeps compiling from it. Delete it by hand when
  that is what you meant.

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
