# Setting up OpenVox Console

A step-by-step guide to standing up the console and the components it
talks to, on real hosts. Every step gives both a **container** and a
**package** path - pick one per component; they interoperate freely (a
packaged openvoxserver works fine with a containerised console, and vice
versa).

For the throwaway local dev stack (one `docker compose up`, self-signed
everything, sample data), use `README.md` instead - this guide is for a
deployment you intend to keep. For running one once it exists - failover,
key rotation, certificate management - see `operations.md`.

---

## 0. What you are building

Five moving parts:

| Component | What it does | Default port |
| --- | --- | --- |
| **openvoxserver** | Compiles catalogs, and is the **certificate authority** every other component's identity comes from | 8140 |
| **openvoxdb** | Stores facts, reports, and package inventory; the console queries it | 8081 (TLS), 8080 (plain, loopback) |
| **openvoxdb's Postgres** | openvoxdb's own storage | 5432 |
| **Console Postgres** | The console's own storage (users, roles, groups, jobs, audit) - separate from openvoxdb's | 5432 |
| **Console** | This project: web UI and API, plus the embedded NATS **node transport** managed nodes connect to | 8080, 8142 |

**Port 8080 collides if you co-locate the console with openvoxdb.**
openvoxdb listens on 8081 (TLS) *and* 8080 (plain HTTP, loopback), and
the console defaults to `:8080`. A wildcard bind conflicts with an
existing loopback bind on the same port, so the console fails to start
with "address already in use". On a shared host, move the console:
`CONSOLE_HTTP_ADDR=:8088` (and match `CONSOLE_BASE_URL`).

Two things are easy to miss and cause most setup failures:

1. **Everything is authenticated by certificates from one CA** - the one
   inside openvoxserver. The console holds three separate identities from
   it (an openvoxdb client cert, a node-transport server cert, and
   optionally a CA-admin cert). Set openvoxserver up first.
2. **Names matter more than IPs.** A certificate is valid for the names
   it was issued with. Decide up front what name each component will be
   reached by *from every side that reaches it*, and put those names in
   the certificates. Retrofitting a name later means reissuing.

### Decide these before you start

| Decision | Used for | Example |
| --- | --- | --- |
| openvoxserver's name, as nodes reach it | Node enrolment | `puppet.example.com` |
| openvoxserver's name, as the console reaches it | openvoxdb/CA client access | same, or an internal name |
| The console's base URL | Install script downloads, links in messages | `https://console.example.com` |
| The node transport address, as nodes dial it | The NATS connection | `console.example.com:8142` |

If the console reaches openvoxserver by a different name than nodes do,
**both** names must be in openvoxserver's certificate.

---

## 1. openvoxserver (do this first - it is the CA)

### Containers

```sh
docker run -d --name openvoxserver \
  -h openvoxserver \
  -p 8140:8140 \
  -e OPENVOXSERVER_HOSTNAME=openvoxserver \
  -e DNS_ALT_NAMES=puppet.example.com,openvoxserver,localhost \
  -e CA_ALLOW_SUBJECT_ALT_NAMES=true \
  -v openvoxserver-ssl:/etc/puppetlabs/puppet/ssl \
  -v openvoxserver-ca:/etc/puppetlabs/puppetserver/ca \
  ghcr.io/avitacco/openvox-console-server:main
```

**Note the image.** That is openvoxserver plus the `enc-bridge` binary
and the `node_terminus = exec` wiring that make the console its node
classifier (step 7). The stock `ghcr.io/openvoxproject/openvoxserver`
image works fine for everything else, but with it node groups silently
classify nothing - the stack looks completely healthy and no error
appears anywhere.

`DNS_ALT_NAMES` must list every name anything will use to reach this
server. `CA_ALLOW_SUBJECT_ALT_NAMES=true` is required for the CA to issue
certificates that carry alt names at all.

Keep both volumes. The CA volume *is* your CA - losing it means every
node has to re-enrol.

### Packages

Add the OpenVox 8 repository, then install:

```sh
# Debian/Ubuntu
. /etc/os-release
curl -fsSL -o /tmp/openvox8-release.deb \
  "https://apt.voxpupuli.org/openvox8-release-${ID}${VERSION_ID}.deb"
sudo apt-get install -y /tmp/openvox8-release.deb
sudo apt-get update && sudo apt-get install -y openvox-server

# RHEL/Rocky/AlmaLinux (el9 shown)
sudo dnf install -y https://yum.voxpupuli.org/openvox8-release-el-9.noarch.rpm
sudo dnf install -y openvox-server
```

Installing the package **already creates the CA and this server's own
certificate**, issued for the host's certname (normally its FQDN). So in
the common case there is nothing else to do but start it:

```sh
sudo systemctl enable --now puppetserver
```

**Do not run `puppetserver ca setup` on a host that already has a CA.**
It is only for creating a CA that does not exist yet, and on an existing
one it stops with `Existing file at .../ca_crt.pem ... If you would
really like to replace your CA, please delete the existing files first`.
Deleting them destroys your CA and invalidates every certificate it has
ever issued - including every enrolled node's. That message is not a
suggestion to follow.

#### Do you need alt names at all?

A certificate is always valid for its own subject. If every component
reaches this server by its certname - a server called
`puppet.example.com` that nodes and the console both address as
`puppet.example.com` - you need no alt names and there is nothing to
change.

You only need them when some name *other than* the certname is used.
Check what the existing certificate already covers:

```sh
sudo openssl x509 -in /etc/puppetlabs/puppet/ssl/certs/$(hostname -f).pem \
  -noout -subject -ext subjectAltName
```

If a name you need is missing, reissue **only this server's own
certificate** - never the CA. Note that `puppet config set dns_alt_names`
affects certificates generated *afterwards*; it does not alter one that
already exists:

```sh
sudo /opt/puppetlabs/bin/puppetserver ca clean --certname $(hostname -f)
sudo /opt/puppetlabs/bin/puppetserver ca generate --certname $(hostname -f) \
  --subject-alt-names puppet.example.com,openvoxserver
sudo systemctl restart puppetserver
```

`ca clean` talks to the CA over HTTPS, so run it with puppetserver up.

On a genuinely fresh host where you want the alt names baked in from the
start, set them before the first start instead, and let the first boot
generate the certificate:

```sh
sudo /opt/puppetlabs/bin/puppet config set --section server \
  dns_alt_names puppet.example.com,openvoxserver
sudo systemctl enable --now puppetserver
```

### Verify

```sh
curl -ksS https://<openvoxserver>:8140/status/v1/simple   # expect: running
```

---

## 2. openvoxdb and its Postgres

### Containers

```sh
docker run -d --name openvoxdb-postgres \
  -e POSTGRES_USER=openvoxdb -e POSTGRES_PASSWORD=<password> \
  -e POSTGRES_DB=openvoxdb \
  -v openvoxdb-postgres-data:/var/lib/postgresql \
  postgres:18

# openvoxdb refuses to start without this extension.
docker exec openvoxdb-postgres \
  psql -U openvoxdb -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'

docker run -d --name openvoxdb \
  -h openvoxdb \
  -p 8081:8081 \
  -e OPENVOXSERVER_HOSTNAME=openvoxserver \
  -e OPENVOXDB_POSTGRES_HOSTNAME=openvoxdb-postgres \
  -e DNS_ALT_NAMES=openvoxdb,localhost \
  -v openvoxdb-ssl:/opt/puppetlabs/server/data/puppetdb/certs \
  ghcr.io/openvoxproject/openvoxdb:latest
```

Keep the `openvoxdb-ssl` volume. Without it, a recreated container has no
keypair, but the CA still remembers a signed certificate for that
certname and refuses to reissue - and the container fails to start.

**openvoxdb enrols itself, and gives up after 120 seconds.** It requests
a certificate from the CA on first boot. If nothing signs that request in
time it fails, and - this is the trap - it writes the failure response
into its own certificate file. Every later restart then sees a non-empty
certificate file, skips enrolment entirely, and puppetdb dies with
`No certs found in 'ssl-cert' file`. The stack never recovers on its own.

`docker-compose.yml` avoids this by autosigning exactly one certname,
`openvoxdb`, through an allowlist (real nodes still need signing). If you
are running the `docker run` commands above instead, sign it yourself
while openvoxdb waits:

```sh
# in a second terminal, right after starting openvoxdb
for i in $(seq 1 24); do
  docker exec openvoxserver puppetserver ca sign --certname openvoxdb && break
  sleep 5
done
```

If you already hit the trap, reset that identity rather than restarting:

```sh
docker rm -f openvoxdb
docker volume rm openvoxdb-ssl
docker exec openvoxserver puppetserver ca clean --certname openvoxdb
# then start openvoxdb again, and sign as above
```

Skipping the `ca clean` gives you `No keypair on disk and CA already has
signed certificate for 'openvoxdb'` on the fresh volume.

### Packages

```sh
sudo apt-get install -y openvoxdb        # or: sudo dnf install -y openvoxdb
```

`puppetdb ssl-setup` does not request a certificate - it copies the ones
this host already has from its Puppet agent. So if openvoxdb is on its
own host, enrol that host with the CA **first**, and sign the request if
the CA does not autosign:

```sh
sudo /opt/puppetlabs/bin/puppet ssl bootstrap --server puppet.example.com
# on the CA, if it did not autosign:
sudo /opt/puppetlabs/bin/puppetserver ca sign --certname <openvoxdb-host>
```

(Skip this when openvoxdb shares a host with openvoxserver - that host
already has a certificate.)

openvoxdb needs a Postgres database of its own. The package does not
create one, and it will not start without the connection configured -
it exits with `No subname set in the "database" config.` and systemd
reports a confusing `protocol`/PID failure as a side effect.

```sh
sudo apt-get install -y postgresql       # or: sudo dnf install -y postgresql-server
sudo systemctl enable --now postgresql
sudo -u postgres createuser --pwprompt puppetdb
sudo -u postgres createdb --owner=puppetdb puppetdb
# openvoxdb refuses to start without this extension. Creating it needs
# superuser rights, so run it as postgres rather than as puppetdb.
sudo -u postgres psql -d puppetdb -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

On Debian/Ubuntu, `systemctl status postgresql` reporting **`active
(exited)` is normal, not a failure** - that unit is a `oneshot` wrapper
that starts the real per-cluster unit and exits. Check the cluster
itself instead, which should read `online`:

```sh
pg_lsclusters
pg_isready          # expect: accepting connections
```

Then write the connection config. `subname` is the part whose absence
produces that error:

```sh
sudo tee /etc/puppetlabs/puppetdb/conf.d/database.ini >/dev/null <<'EOF'
[database]
subname = //localhost:5432/puppetdb
username = puppetdb
password = <the password set above>
EOF
sudo chown puppetdb:puppetdb /etc/puppetlabs/puppetdb/conf.d/database.ini
sudo chmod 640 /etc/puppetlabs/puppetdb/conf.d/database.ini
```

Use the Postgres host's name instead of `localhost` in `subname` if the
database is on another machine. Finally copy the certificates into place
and start it:

```sh
sudo /opt/puppetlabs/bin/puppetdb ssl-setup
sudo systemctl enable --now puppetdb
```

If it fails to start, `/var/log/puppetlabs/puppetdb/puppetdb.log` has
the real reason - the systemd unit's own error is usually just fallout
from the process exiting.

On the openvoxserver host, install the terminus so catalogs and reports
are actually sent to openvoxdb:

```sh
sudo apt-get install -y openvoxdb-termini    # or dnf
```

### Verify

```sh
curl -ksS https://<openvoxdb>:8081/status/v1/simple   # expect: running
```

---

## 3. The console's own Postgres

Separate database from openvoxdb's. The console creates its own schema on
first start - you only need an empty database and a role that owns it.

### Containers

```sh
docker run -d --name console-postgres \
  -e POSTGRES_USER=console -e POSTGRES_PASSWORD=<password> \
  -e POSTGRES_DB=console \
  -p 5432:5432 \
  -v console-postgres-data:/var/lib/postgresql \
  postgres:18
```

### Packages

```sh
sudo apt-get install -y postgresql        # or: sudo dnf install -y postgresql-server
sudo -u postgres createuser --pwprompt console
sudo -u postgres createdb --owner=console console
```

**If the console runs on a different host**, Postgres also has to listen
on the network - Debian/Ubuntu bind `localhost` only by default, so a
remote connection fails with `Connection refused` even though
`pg_isready` passes locally:

```sh
sudo -u postgres psql -c "ALTER SYSTEM SET listen_addresses = '*';"
# allow the console's host or subnet - adjust the CIDR:
echo 'host console console 10.0.0.0/24 scram-sha-256' \
  | sudo tee -a /etc/postgresql/*/main/pg_hba.conf
sudo systemctl restart postgresql@*-main.service
```

Use `sslmode=require` in the DSN once the connection leaves the host.

### Verify

```sh
PGPASSWORD='<password>' psql -h <host> -U console -d console -c 'select 1;'
```

Single quotes matter: a password containing `$` is otherwise expanded by
the shell before psql sees it. For the same reason, prefer this form over
a `postgres://user:password@host/db` URI, where `$`, `@`, `:` and `/` all
have to be percent-encoded as well.

---

## 4. Issue the console's credentials

The console needs three identities, all signed by openvoxserver's CA.
Run `puppetserver ca` commands on the openvoxserver host (or
`docker exec openvoxserver ...` for the container path).

### 4a. openvoxdb client certificate (required)

How the console authenticates to openvoxdb:

```sh
puppetserver ca generate --certname console
```

Copy three files to the console host - the certificate, its key, and the
CA bundle:

```
/etc/puppetlabs/puppet/ssl/certs/console.pem          -> console-cert.pem
/etc/puppetlabs/puppet/ssl/private_keys/console.pem   -> console-key.pem
/etc/puppetlabs/puppet/ssl/certs/ca.pem               -> ca.pem
```

### 4b. RBAC signing key (required)

The console signs its own JWTs with an ES256 key. This one is not from
the CA - generate it locally:

```sh
openssl ecparam -genkey -name prime256v1 -noout -out rbac-signing-key.pem
```

Back this up. Losing it invalidates every issued token (everyone is
logged out); leaking it lets anyone mint tokens. Rotation is documented
in `operations.md`.

### 4c. Node transport server certificate (required for orchestration)

The TLS certificate nodes verify when they connect to the console's NATS
transport. **It must carry the exact name nodes will dial** - that is the
host part of `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`:

```sh
puppetserver ca generate --certname node-transport \
  --subject-alt-names console.example.com,node-transport
```

Copy out the certificate and key the same way as 4a. Nodes authenticate
back with their own Puppet certificates, which is why the CA bundle is
the same one.

### 4d. CA admin certificate (optional - certificate management in the UI)

Only needed for the Nodes page to show certificate status and to
sign/revoke/clean certificates from the console.

**This credential is full CA admin.** Puppet Server gates every CA
endpoint behind one authorization extension, so a certificate that can
read statuses can also sign, revoke, and clean *any* node's certificate.
There is no read-only variant. See `operations.md` before issuing it.

Generation writes to CA state that a running server also writes, so stop
the server first:

```sh
# container path
docker stop openvoxserver
docker run --rm --entrypoint "" \
  -v openvoxserver-ssl:/etc/puppetlabs/puppet/ssl \
  -v openvoxserver-ca:/etc/puppetlabs/puppetserver/ca \
  ghcr.io/openvoxproject/openvoxserver:latest \
  puppetserver ca generate --ca-client --certname console-ca-client
docker start openvoxserver

# package path
sudo systemctl stop puppetserver
sudo puppetserver ca generate --ca-client --certname console-ca-client
sudo systemctl start puppetserver
```

The output path is version-dependent - check
`puppetserver ca generate --help`.

---

## 5. Configure the console

Configuration is environment variables only. **Six are required** - the
console refuses to start without them, naming the one it is missing:

```sh
CONSOLE_POSTGRES_DSN=postgres://console:<password>@<pg-host>:5432/console?sslmode=require
CONSOLE_OPENVOXDB_URL=https://<openvoxdb-host>:8081
CONSOLE_OPENVOXDB_CERT_FILE=/etc/openvox-console/console-cert.pem
CONSOLE_OPENVOXDB_KEY_FILE=/etc/openvox-console/console-key.pem
CONSOLE_OPENVOXDB_CA_FILE=/etc/openvox-console/ca.pem
CONSOLE_RBAC_SIGNING_KEY_FILE=/etc/openvox-console/rbac-signing-key.pem
```

Everything else is optional, and each optional feature degrades on its
own rather than blocking startup. The ones that matter for a real
deployment:

```sh
# Where the console listens, and how the outside world reaches it.
CONSOLE_HTTP_ADDR=:8080
CONSOLE_BASE_URL=https://console.example.com

# The first admin user, created only while the users table is empty.
# Remove these after first start; change the password immediately.
CONSOLE_BOOTSTRAP_ADMIN_USERNAME=admin
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD=<strong password>

# Node transport (orchestration). ADDR is what the console binds;
# PUBLIC_ADDR is what a remote node dials - they differ, and PUBLIC_ADDR's
# host must be a name in the 4c certificate.
CONSOLE_NODE_TRANSPORT_ADDR=:8142
CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR=console.example.com:8142
CONSOLE_NODE_TRANSPORT_CERT_FILE=/etc/openvox-console/node-transport-cert.pem
CONSOLE_NODE_TRANSPORT_KEY_FILE=/etc/openvox-console/node-transport-key.pem
CONSOLE_NODE_TRANSPORT_CA_FILE=/etc/openvox-console/ca.pem

# The Puppet server a NODE should enrol against - node-facing, so not the
# same value as CA_CLIENT_URL below. Must match a name in openvoxserver's
# certificate. Unset means the install script leaves a node's existing
# Puppet server config alone.
CONSOLE_PUPPET_SERVER_PUBLIC_ADDR=puppet.example.com

# Certificate management in the UI (the 4d credential).
CONSOLE_CA_CLIENT_URL=https://<openvoxserver>:8140
CONSOLE_CA_CLIENT_CERT_FILE=/etc/openvox-console/ca-client-cert.pem
CONSOLE_CA_CLIENT_KEY_FILE=/etc/openvox-console/ca-client-key.pem
CONSOLE_CA_CLIENT_CA_FILE=/etc/openvox-console/ca.pem
```

### Keeping secrets out of the environment

Any setting can be read from a file instead, by naming the file in
`<SETTING>_FILE`. The file's contents are used (trailing newline
trimmed), and `<SETTING>_FILE` wins if both are set. A file that is named
but unreadable is fatal - the console will not start with the value
silently empty.

```sh
# instead of CONSOLE_POSTGRES_DSN=postgres://console:hunter2@db/console
CONSOLE_POSTGRES_DSN_FILE=/run/secrets/console_postgres_dsn
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/console_bootstrap_admin_password
```

This is what lets the compose stack use Docker secrets, and it works the
same for a systemd unit pointed at files on disk. `enc-bridge` reads
`ENC_BRIDGE_TOKEN_FILE` the same way.

Optional subsystems documented elsewhere: OIDC login
(`CONSOLE_OIDC_*`), code deployment (`CONSOLE_G10K_BIN_PATH`,
`CONSOLE_CONTROL_REPO_URL`, `CONSOLE_CODE_WEBHOOK_SECRET`,
`CONSOLE_CODE_DIR_PATH`), and audit levels (`CONSOLE_AUDIT_*`,
`CONSOLE_AUDIT_LOG_PATH`). `internal/runtime/config.go` is the
authoritative list of what is read.

---

## 6. Run the console

There is no `.deb`/`.rpm` for the console itself - it is a published
container image or a single static binary. (The *node* agent does have
packages; that is step 8.)

### Containers

The image is built and published by this project's own CI on every push
to `main`, for `linux/amd64` and `linux/arm64`:

```
ghcr.io/avitacco/openvox-console
```

**Which tag to use.** `:main` tracks the latest commit on `main` and is
the compose file's default. `:latest` and semver tags
(`:1.2.3`, `:1.2`) are published only when a `v*` git tag is pushed - so
until the first release is cut, **`:latest` does not exist** and asking
for it fails with `manifest unknown`. Every build also publishes an
immutable `:sha-<commit>` tag, which is what to pin for a deployment you
want to hold still:

```sh
CONSOLE_IMAGE_TAG=sha-a1b2c3d      # exact commit
CONSOLE_IMAGE_TAG=v1.2.3           # once releases exist
```

`docker-compose.yml` in this repo is the production stack - console,
both databases, openvoxserver and openvoxdb. It pulls the image above;
nothing is built on the deployment host. Copy that one file across:

```sh
mkdir -p /opt/openvox-console/{secrets,certs}
cp docker-compose.yml /opt/openvox-console/
cd /opt/openvox-console
```

Put the step 4 credentials in `certs/`, and the secrets in `secrets/`
as one value per file, no trailing newline needed:

```sh
printf 'postgres://console:<password>@console-postgres:5432/console?sslmode=require' \
  > secrets/console_postgres_dsn
printf '<same password>'      > secrets/console_postgres_password
printf '<strong password>'    > secrets/console_bootstrap_admin_password
printf '<enc:read token>'     > secrets/enc_bridge_token
chmod 600 secrets/*
```

Set the deployment's own variables in a `.env` beside it
(`CONSOLE_BASE_URL`, `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`,
`OPENVOXDB_POSTGRES_PASSWORD`, plus `CONSOLE_IMAGE_TAG` to pin the
console and `OPENVOX_VERSION` to pin openvoxserver/openvoxdb), then:

```sh
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
```

To upgrade later, change `CONSOLE_IMAGE_TAG` (or re-`pull` if you track
`:main`) and `up -d` again - Compose recreates only what changed.

**Always pass `-f docker-compose.yml` explicitly.** A bare
`docker compose up` also loads `docker-compose.override.yml`, which is
the development stack - host-published database ports, throwaway
passwords, autosign on, repo bind mounts. The `-f` form loads only the
production file. For the same reason, do not deploy from a checkout of
this repo: it carries both that override and a dev `.env` whose values
would be picked up silently.

The compose file refuses to start if `CONSOLE_BASE_URL`,
`CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR` or `OPENVOXDB_POSTGRES_PASSWORD`
are unset, so a deployment cannot come up on a placeholder.

### Binary

Use this when you would rather run the console under systemd than in a
container. Building needs Go and Node; the Dockerfile route needs
neither beyond Docker itself.

```sh
make build             # produces bin/console (and bin/enc-bridge)
sudo install -m 0755 bin/console /usr/local/bin/console
```

`/etc/systemd/system/openvox-console.service`:

```ini
[Unit]
Description=OpenVox Console
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/openvox-console/console.env
ExecStart=/usr/local/bin/console
Restart=on-failure
User=openvox-console
Group=openvox-console

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl enable --now openvox-console
```

The credential files must be readable by the `openvox-console` user and
by nobody else.

### First start

The console creates and migrates its own schema, then creates the
bootstrap admin if the users table is empty. Expect three log lines:

```
database migrations up to date
node transport ready   addr=[::]:8142
console ready          addr=:8080
```

Log in at `CONSOLE_BASE_URL`, change the bootstrap password, then remove
`CONSOLE_BOOTSTRAP_ADMIN_*` from the environment.

---

## 7. Connect openvoxserver to the console (classification)

openvoxserver asks the console what classes a node gets, using Puppet's
standard `exec` node terminus. `cmd/enc-bridge` is the small binary that
bridges the two.

**Issue it a token first.** A service token carries a fixed permission
set chosen at creation - no role needed. Create one scoped to `enc:read`:

```sh
curl -X POST "$CONSOLE_BASE_URL/api/v1/service-tokens" \
  -H "Authorization: Bearer <admin access token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"enc-bridge","permissions":["enc:read"]}'
```

The response's `token` field is shown **once** - copy it now.

### Containers

The binary and the `puppet.conf` wiring are already in the
`openvox-console-server` image from step 1 - there is nothing to install.
You only supply the token.

`docker-compose.yml` reads it from the `enc_bridge_token` secret file:

```sh
printf '%s' '<the enc:read service token>' > secrets/enc_bridge_token
```

**Permissions matter here, and differently from the other secrets.**
puppetserver runs as `puppet:0`, not root, and Compose bind-mounts secret
files with their host ownership - so a `chmod 600` file owned by your
login user is unreadable inside the container and classification fails.
Give group 0 read access, and protect the directory instead:

```sh
sudo chgrp 0 secrets/enc_bridge_token
chmod 640 secrets/enc_bridge_token
chmod 700 secrets
```

Then recreate the container so it picks up the new token:

```sh
docker compose -f docker-compose.yml up -d --force-recreate openvoxserver
```

### Packages

Install the bridge on the **openvoxserver** host and configure it:

```sh
sudo install -m 0755 bin/enc-bridge /usr/local/bin/enc-bridge
```

enc-bridge reads two environment variables:

```sh
ENC_BRIDGE_URL=https://console.example.com
ENC_BRIDGE_TOKEN=<the enc:read service token>
```

Then point Puppet at it in `/etc/puppetlabs/puppet/puppet.conf`:

```ini
[server]
node_terminus = exec
external_nodes = /usr/local/bin/enc-bridge
```

Restart openvoxserver.

### Verify

Ask the bridge directly, as puppetserver runs it. This checks the binary,
the token, the permissions and the console's reply in one command:

```sh
# container path
docker compose -f docker-compose.yml exec -u puppet:0 openvoxserver \
  /usr/local/bin/enc-bridge <a certname>

# package path
sudo -u puppet /usr/local/bin/enc-bridge <a certname>
```

`{}` means it worked and that node matches no group yet. Create a node
group in the console (Groups page) that pins that certname, run the same
command again, and you should get its classes back:

```yaml
classes:
    ntp:
        servers:
            - time.example.com
parameters:
    role: prodtest
environment: production
```

An error here is the real failure mode to catch: `permission denied`
means the token file is not readable by `puppet:0`, and a 401 means the
token is wrong or lacks `enc:read`.

Then run `puppet agent -t` on that node. If it fails with `Could not find
class <name>`, classification is working - the ENC told puppetserver to
include a class your Puppet code does not define yet. puppetserver caches
environments, so after adding the code, flush the cache or the same error
persists:

```sh
docker compose -f docker-compose.yml exec openvoxserver sh -c \
  'curl -ksS -X DELETE \
     --cert /etc/puppetlabs/puppet/ssl/certs/openvoxserver.pem \
     --key /etc/puppetlabs/puppet/ssl/private_keys/openvoxserver.pem \
     https://127.0.0.1:8140/puppet-admin-api/v1/environment-cache'
```

---

## 8. Enrol nodes

### If you built the binary yourself, build the agent packages too

The console serves `node-agent-client` from its own apt/yum repository
routes, and those artifacts are embedded into the console binary at
build time rather than fetched at runtime. They are generated, not
committed.

**Using the published image? Nothing to do** - the Dockerfile builds the
per-platform agent binaries and their `.deb`/`.rpm` packages as part of
the image build, so they are already inside it.

If you built the binary yourself (the `make build` route in step 6),
build them first or the repository routes return 404 and every node
install fails:

```sh
make agent-binaries    # cross-compiles node-agent-client for each platform
make agent-packages    # builds the .deb/.rpm the console will serve
make build             # rebuild the console so it embeds them
```

### Run the install script on each node

```sh
# Linux
curl -fsSL https://console.example.com/packages/install.sh | sudo bash

# macOS
curl -fsSL https://console.example.com/packages/install-macos.sh | sudo bash
```

```powershell
# Windows (elevated PowerShell)
Invoke-WebRequest https://console.example.com/packages/install.ps1 -OutFile install.ps1
.\install.ps1
```

One run does everything: installs openvoxagent, **enrols the node with
the CA if it has no certificate yet**, installs node-agent-client from
the console's repository, and registers it as a service. It is safe to
re-run.

### Signing

If your CA autosigns, the script completes unattended. If it does not,
the script waits up to 5 minutes, polling every 15 seconds - sign the
request on the console's **Nodes** page while it waits and the same run
continues to completion. If the wait expires, the script says so and
names the page; sign it, then re-run the script and it picks up from
there.

A failure to *reach* the CA is reported as that, distinctly from a
request waiting to be signed - so if you see a connectivity error, check
name resolution and port 8140 rather than hunting for a pending request.

### Verify

The Nodes page should show the node as connected, with a signed
certificate. After its first Puppet run it also gets facts, reports, and
package inventory.

---

## 9. Verification checklist

| Check | Expectation |
| --- | --- |
| `curl -ksS https://<openvoxserver>:8140/status/v1/simple` | `running` |
| `curl -ksS https://<openvoxdb>:8081/status/v1/simple` | `running` |
| `curl -fsS <CONSOLE_BASE_URL>/health` | `{"status":"ok","checks":{"nats":"ok","postgres":"ok"}}` |
| Console log on start | migrations, node transport, console ready |
| Log in as the bootstrap admin | succeeds; password changed afterwards |
| Nodes page | your nodes, connected, certificates signed |
| A node's page | facts, reports, package inventory after a Puppet run |
| Groups page | a group matches the nodes you expect |

---

## Where to go next

- `operations.md` - running it: Postgres failover, JWT key rotation,
  certificate management, node enrolment details, node deletion.
- `README.md` - local development stack and per-feature notes.
- `architecture-summary.md` - why the pieces fit together this way.
- `openspec/specs/` - the behaviour specification.
