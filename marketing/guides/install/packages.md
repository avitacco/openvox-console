---
title: Install from packages and binaries
summary: Install openvoxserver, openvoxdb and Postgres from packages, build the console, and run it as a systemd service.
order: 3
---

This guide installs openvoxserver, openvoxdb and Postgres from the
OpenVox and distribution packages, and runs the console as a single
binary under systemd. Settle the names in [Plan a deployment](plan.html)
before you start.

The commands are for Debian and Ubuntu, with the RHEL, Rocky Linux and
AlmaLinux equivalent alongside where it differs. The parts can live on
one host or several; where a step belongs on a particular host, it says
so.

## Install openvoxserver

openvoxserver is the certificate authority every other part's
certificate comes from, so it comes first. Add the OpenVox 8 repository
and install it:

```sh tab="Debian and Ubuntu"
. /etc/os-release
curl -fsSL -o /tmp/openvox8-release.deb \
  "https://apt.voxpupuli.org/openvox8-release-${ID}${VERSION_ID}.deb"
sudo apt-get install -y /tmp/openvox8-release.deb
sudo apt-get update && sudo apt-get install -y openvox-server
```

```sh tab="RHEL, Rocky and AlmaLinux"
sudo dnf install -y https://yum.voxpupuli.org/openvox8-release-el-9.noarch.rpm
sudo dnf install -y openvox-server
```

Installing the package **already creates the CA and this server's own
certificate**, issued for the host's certname, normally its fully
qualified name. In the common case there is nothing more to do than start
it:

```sh
sudo systemctl enable --now puppetserver
```

> [!CAUTION]
> Do not run `puppetserver ca setup` on a host that already has a CA. It
> only creates a CA that does not exist yet, and on an existing one it
> stops with "Existing file at .../ca_crt.pem". Deleting those files as
> the message suggests destroys your CA and every certificate it has ever
> issued, including every enrolled node's.

### Check its certificate's names

A certificate is always valid for its own certname. If nodes and the
console both reach this server by its certname, there is nothing to
change. Otherwise, check which names the certificate already covers:

```sh
sudo openssl x509 -in /etc/puppetlabs/puppet/ssl/certs/$(hostname -f).pem \
  -noout -subject -ext subjectAltName
```

If a name from your plan is missing, reissue **only this server's own
certificate**, never the CA. Setting `dns_alt_names` afterwards does not
change a certificate that already exists, so clean and regenerate it
while the server is running:

```sh
sudo /opt/puppetlabs/bin/puppetserver ca clean --certname $(hostname -f)
sudo /opt/puppetlabs/bin/puppetserver ca generate --certname $(hostname -f) \
  --subject-alt-names puppet.example.com,openvoxserver
sudo systemctl restart puppetserver
```

> [!TIP]
> On a fresh host, you can set the names before the first start instead,
> and let that start create the certificate with them:
> `sudo /opt/puppetlabs/bin/puppet config set --section server dns_alt_names puppet.example.com,openvoxserver`

### Check it is running

```sh
curl -ksS https://puppet.example.com:8140/status/v1/simple
```

The reply is `running`.

## Install openvoxdb and its database

### Enrol openvoxdb's host

openvoxdb does not request a certificate itself: its setup copies the one
its host already has. If openvoxdb has a host of its own, enrol that host
with the CA first, then sign its request on the CA if the CA does not sign
automatically:

```sh
sudo /opt/puppetlabs/bin/puppet ssl bootstrap --server puppet.example.com

# on the CA host:
sudo /opt/puppetlabs/bin/puppetserver ca sign --certname <openvoxdb host>
```

Skip this when openvoxdb shares a host with openvoxserver, which already
has a certificate.

### Create its database

openvoxdb needs a Postgres database of its own, which the package does not
create:

```sh tab="Debian and Ubuntu"
sudo apt-get install -y postgresql
sudo systemctl enable --now postgresql
```

```sh tab="RHEL, Rocky and AlmaLinux"
sudo dnf install -y postgresql-server
sudo postgresql-setup --initdb
sudo systemctl enable --now postgresql
```

```sh
sudo -u postgres createuser --pwprompt puppetdb
sudo -u postgres createdb --owner=puppetdb puppetdb
# openvoxdb refuses to start without this extension, and creating it
# needs superuser rights - so it is run as postgres, not as puppetdb.
sudo -u postgres psql -d puppetdb -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

Check Postgres is accepting connections:

```sh
pg_isready
```

The reply ends in `accepting connections`.

> [!NOTE]
> On Debian and Ubuntu, `systemctl status postgresql` reads
> `active (exited)`, and that is normal: that unit only starts the real
> per-cluster one. `pg_lsclusters` shows the cluster itself as `online`.

### Install and start openvoxdb

```sh tab="Debian and Ubuntu"
sudo apt-get install -y openvoxdb
```

```sh tab="RHEL, Rocky and AlmaLinux"
sudo dnf install -y openvoxdb
```

Tell it where its database is. Without this file it will not start, and
exits with `No subname set in the "database" config.`:

```ini
[database]
subname = //localhost:5432/puppetdb
username = puppetdb
password = <the password you set above>
```

Save that as `/etc/puppetlabs/puppetdb/conf.d/database.ini`, using the
database host's name instead of `localhost` if it is elsewhere, and
restrict it:

```sh
sudo chown puppetdb:puppetdb /etc/puppetlabs/puppetdb/conf.d/database.ini
sudo chmod 640 /etc/puppetlabs/puppetdb/conf.d/database.ini
```

Then copy the host's certificates into place and start it:

```sh
sudo /opt/puppetlabs/bin/puppetdb ssl-setup
sudo systemctl enable --now puppetdb
```

If it fails to start, `/var/log/puppetlabs/puppetdb/puppetdb.log` holds
the real reason; systemd's own error is usually just the consequence.

### Send catalogs and reports to it

On the openvoxserver host, install the terminus that sends catalogs and
reports to openvoxdb:

```sh tab="Debian and Ubuntu"
sudo apt-get install -y openvoxdb-termini
```

```sh tab="RHEL, Rocky and AlmaLinux"
sudo dnf install -y openvoxdb-termini
```

### Check it is running

```sh
curl -ksS https://<openvoxdb host>:8081/status/v1/simple
```

The reply is `running`.

## Create the console's database

The console keeps its own data in a database separate from openvoxdb's.
It creates its schema itself on first start; all it needs is an empty
database and a role that owns it. On the Postgres host:

```sh
sudo -u postgres createuser --pwprompt console
sudo -u postgres createdb --owner=console console
```

If the console will run on a different host, Postgres also has to listen
on the network. Debian and Ubuntu listen on `localhost` only, so a remote
connection is refused even though `pg_isready` passes locally:

```sh
sudo -u postgres psql -c "ALTER SYSTEM SET listen_addresses = '*';"
# allow the console's host or subnet - adjust the address range:
echo 'host console console 10.0.0.0/24 scram-sha-256' \
  | sudo tee -a /etc/postgresql/*/main/pg_hba.conf
sudo systemctl restart postgresql@*-main.service
```

Check the console's role can connect, from the console's host:

```sh
PGPASSWORD='<password>' psql -h <postgres host> -U console -d console -c 'select 1;'
```

> [!TIP]
> Keep the single quotes. A password containing `$` is otherwise expanded
> by the shell before psql sees it.

## Issue the console's certificates

The console needs two certificates from the CA, and optionally a third.
Generate them on the openvoxserver host, then copy them to the console's
host, into `/etc/openvox-console/`.

### The openvoxdb client certificate

This is how the console authenticates to openvoxdb:

```sh
sudo /opt/puppetlabs/bin/puppetserver ca generate --certname console
```

It writes three files the console needs. Copy them across under these
names:

| On the openvoxserver host | On the console host |
| --- | --- |
| `/etc/puppetlabs/puppet/ssl/certs/console.pem` | `/etc/openvox-console/console-cert.pem` |
| `/etc/puppetlabs/puppet/ssl/private_keys/console.pem` | `/etc/openvox-console/console-key.pem` |
| `/etc/puppetlabs/puppet/ssl/certs/ca.pem` | `/etc/openvox-console/ca.pem` |

### The node transport certificate

Nodes check this certificate when they connect to the console, so it must
carry the exact name they dial: the host part of
`CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`.

```sh
sudo /opt/puppetlabs/bin/puppetserver ca generate --certname node-transport \
  --subject-alt-names console.example.com,node-transport
```

Copy `certs/node-transport.pem` and `private_keys/node-transport.pem`
across the same way, as `node-transport-cert.pem` and
`node-transport-key.pem`. Nodes authenticate back with their own
certificates from the same CA, so the console needs nothing else for
them.

<!-- include: rbac-key.md -->

Generate it on the console's host, into `/etc/openvox-console/`.

<!-- include: ca-client-intro.md -->

Issuing it writes to CA state that the running server also writes, so
stop openvoxserver while it is issued:

```sh
sudo systemctl stop puppetserver
sudo /opt/puppetlabs/bin/puppetserver ca generate --ca-client --certname console-ca-client
sudo systemctl start puppetserver
```

Copy `certs/console-ca-client.pem` and `private_keys/console-ca-client.pem`
across as `ca-client-cert.pem` and `ca-client-key.pem`. Then confirm the
CA accepts it, from the console's host:

```sh
curl -sS -o /dev/null -w 'HTTP %{http_code}\n' \
  --cert /etc/openvox-console/ca-client-cert.pem \
  --key /etc/openvox-console/ca-client-key.pem \
  --cacert /etc/openvox-console/ca.pem \
  https://puppet.example.com:8140/puppet-ca/v1/certificate_statuses/all
```

`HTTP 200` means it works.

## Build the console

The console is one binary with its web interface built in. There is no
package of it; build it from the source repository. You need Go at the
version named in `go.mod`, and git.

```sh
git clone https://github.com/avitacco/openvox-console.git
cd openvox-console
```

The console also serves the node agent that nodes install, from its own
package repository, and those agents are built into the console binary.
Build them first, or every node install fails with a 404:

```sh
make agent-binaries
make agent-packages
make build
```

That leaves `bin/console` and `bin/enc-bridge`. Install the console on its
host:

```sh
sudo install -m 0755 bin/console /usr/local/bin/console
```

## Configure the console

The console is configured with environment variables only. Create a user
for it, and a configuration file only that user can read:

```sh
sudo useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin openvox-console
sudo install -d -m 0750 -o root -g openvox-console /etc/openvox-console
```

Six settings are required, and the console refuses to start without them,
naming the one that is missing. Put them in
`/etc/openvox-console/console.env`:

```ini
CONSOLE_POSTGRES_DSN=postgres://console:<password>@<postgres host>:5432/console?sslmode=require
CONSOLE_OPENVOXDB_URL=https://<openvoxdb host>:8081
CONSOLE_OPENVOXDB_CERT_FILE=/etc/openvox-console/console-cert.pem
CONSOLE_OPENVOXDB_KEY_FILE=/etc/openvox-console/console-key.pem
CONSOLE_OPENVOXDB_CA_FILE=/etc/openvox-console/ca.pem
CONSOLE_RBAC_SIGNING_KEY_FILE=/etc/openvox-console/rbac-signing-key.pem
```

> [!NOTE]
> `sslmode=require` assumes your Postgres serves TLS, which it should
> once the connection leaves the host. If the console and Postgres share
> a host and Postgres has no certificate, use `sslmode=disable`.

The rest is optional, and each optional feature switches itself off
rather than blocking startup. For a real deployment, add:

```ini
# Where the console listens, and how the outside world reaches it.
CONSOLE_HTTP_ADDR=:8080
CONSOLE_BASE_URL=https://console.example.com

# The first admin user, created only while there are no users.
CONSOLE_BOOTSTRAP_ADMIN_USERNAME=admin
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD=<a strong password>

# The node transport. ADDR is what the console binds; PUBLIC_ADDR is what
# a node dials, and its host must be in the node transport certificate.
CONSOLE_NODE_TRANSPORT_ADDR=:8142
CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR=console.example.com:8142
CONSOLE_NODE_TRANSPORT_CERT_FILE=/etc/openvox-console/node-transport-cert.pem
CONSOLE_NODE_TRANSPORT_KEY_FILE=/etc/openvox-console/node-transport-key.pem
CONSOLE_NODE_TRANSPORT_CA_FILE=/etc/openvox-console/ca.pem

# The Puppet server a node should enrol against, written into the node
# install script.
CONSOLE_PUPPET_SERVER_PUBLIC_ADDR=puppet.example.com

# Certificate management from the Nodes page (the optional credential).
CONSOLE_CA_CLIENT_URL=https://puppet.example.com:8140
CONSOLE_CA_CLIENT_CERT_FILE=/etc/openvox-console/ca-client-cert.pem
CONSOLE_CA_CLIENT_KEY_FILE=/etc/openvox-console/ca-client-key.pem
CONSOLE_CA_CLIENT_CA_FILE=/etc/openvox-console/ca.pem
```

### Keep secrets in files

Any setting can be read from a file instead: name the file in the
setting's name followed by `_FILE`. That keeps passwords out of the
process environment:

```ini
CONSOLE_POSTGRES_DSN_FILE=/etc/openvox-console/postgres-dsn
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD_FILE=/etc/openvox-console/bootstrap-admin-password
```

A trailing newline in the file is ignored. A file that is named but
cannot be read stops the console from starting, rather than letting it
start with the value silently empty.

Finally, make every file readable by the console's user and nobody else:

```sh
sudo chown root:openvox-console /etc/openvox-console/*
sudo chmod 0640 /etc/openvox-console/*
```

## Run it under systemd

Create `/etc/systemd/system/openvox-console.service`:

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

Then start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now openvox-console
journalctl -u openvox-console -f
```

<!-- include: first-start.md -->

<!-- include: enc-token.md -->

### Install enc-bridge on openvoxserver's host

Copy `bin/enc-bridge` from your build to the openvoxserver host and
install it:

```sh
sudo install -m 0755 enc-bridge /usr/local/bin/enc-bridge
```

Save the token where only openvoxserver's user can read it:

```sh
printf '%s' '<the enc:read service token>' | sudo tee /etc/puppetlabs/enc-bridge-token >/dev/null
sudo chown root:puppet /etc/puppetlabs/enc-bridge-token
sudo chmod 0640 /etc/puppetlabs/enc-bridge-token
```

enc-bridge reads where the console is, and its token, from its
environment, which it inherits from openvoxserver. Give openvoxserver
both, with `sudo systemctl edit puppetserver`:

```ini
[Service]
Environment=ENC_BRIDGE_URL=https://console.example.com
Environment=ENC_BRIDGE_TOKEN_FILE=/etc/puppetlabs/enc-bridge-token
```

Then point Puppet at it, in the `[server]` section of
`/etc/puppetlabs/puppet/puppet.conf`, and restart openvoxserver:

```ini
[server]
node_terminus = exec
external_nodes = /usr/local/bin/enc-bridge
```

```sh
sudo systemctl restart puppetserver
```

### Check classification

Run enc-bridge the way openvoxserver does: as its user, with the same
settings. This checks the program, the token, the file permissions and
the console's answer in one command:

```sh
sudo -u puppet env ENC_BRIDGE_URL=https://console.example.com \
  ENC_BRIDGE_TOKEN_FILE=/etc/puppetlabs/enc-bridge-token \
  /usr/local/bin/enc-bridge <a certname>
```

<!-- include: enc-verify.md -->

## Upgrade later

To move to a newer console, pull the source, rebuild in the same order,
install the new binary and restart:

```sh
git pull
make agent-binaries
make agent-packages
make build
sudo install -m 0755 bin/console /usr/local/bin/console
sudo systemctl restart openvox-console
```

<!-- include: checklist.md -->
