---
title: Install with containers
summary: Run openvoxserver, openvoxdb, both databases and the console with Docker Compose, from an empty host to a console ready for nodes.
order: 2
---

This guide runs the whole stack from the project's production compose
file and published images. Nothing is built on the host. Settle the names
in [Plan a deployment](plan.html) before you start.

You need a Linux host with Docker Engine and the Compose plugin.

## Get the compose file

`docker-compose.yml` in the project repository is the production stack:
the console, both databases, openvoxserver and openvoxdb. Copy that one
file to the host, into a directory of its own:

```sh
mkdir -p /opt/openvox-console/secrets /opt/openvox-console/certs
cd /opt/openvox-console
curl -fsSLO https://raw.githubusercontent.com/avitacco/openvox-console/main/docker-compose.yml
```

> [!WARNING]
> Do not deploy from a checkout of the repository. A checkout also
> carries `docker-compose.override.yml`, the development stack, which a
> bare `docker compose up` loads silently: throwaway passwords, open
> database ports and automatic certificate signing. Every command in
> this guide passes `-f docker-compose.yml` so that only the production
> file is ever read.

## Set the deployment's own values

Create `.env` beside the compose file. The compose file refuses to start
while any of the first three is unset, so a deployment cannot come up on a
placeholder:

```sh
CONSOLE_BASE_URL=https://console.example.com
CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR=console.example.com:8142
OPENVOXDB_POSTGRES_PASSWORD=<a strong password>

# Every name nodes or the console use to reach openvoxserver, beyond its
# own name "openvoxserver" - see Plan a deployment.
OPENVOXSERVER_DNS_ALT_NAMES=puppet.example.com

# The Puppet server a node should enrol against, written into the node
# install script. Must be one of the names above.
CONSOLE_PUPPET_SERVER_PUBLIC_ADDR=puppet.example.com
```

Choose which console image to run. `CONSOLE_IMAGE_TAG` defaults to
`main`, which follows the latest commit. For a deployment you want to hold
still, pin the immutable tag of one commit instead:

```sh
CONSOLE_IMAGE_TAG=sha-a1b2c3d
```

> [!NOTE]
> Version tags such as `v1.2.3`, and `latest`, are published only once a
> release is cut. Until then, asking for `latest` fails with
> `manifest unknown`.

## Write the secrets

Secrets are files in `secrets/`, one value per file, rather than
environment variables, so they never appear in `docker inspect`:

```sh
printf '%s' '<console database password>' > secrets/console_postgres_password
printf 'postgres://console:%s@console-postgres:5432/console?sslmode=disable' '<console database password>' \
  > secrets/console_postgres_dsn
printf '%s' '<a strong password>' > secrets/console_bootstrap_admin_password
chmod 600 secrets/*
```

The console's database connection string must use the same password as
`console_postgres_password`. `sslmode=disable` is deliberate: the
Postgres container runs without TLS, and the connection never leaves the
compose network, so asking for TLS would only make the console fail to
connect.

openvoxserver also needs a token file, `enc_bridge_token`, which you
fill in once the console is running. Create it empty for now, because
openvoxserver will not start without it:

```sh
: > secrets/enc_bridge_token
```

## Start the certificate authority

openvoxserver is the CA every other part's certificate comes from, so it
starts first and on its own:

```sh
docker compose -f docker-compose.yml up -d openvoxserver
```

Its first start creates the CA, which takes a minute or two. Wait until
it reports itself running:

```sh
docker compose -f docker-compose.yml exec openvoxserver \
  curl -ksS https://127.0.0.1:8140/status/v1/simple
```

The reply is `running`.

> [!CAUTION]
> The `openvoxserver-ca` volume *is* your certificate authority. Losing
> it means every node has to enrol again. Back it up, and never remove it
> with `docker compose down -v`.

## Issue the console's certificates

The console needs two certificates from the CA, and optionally a third.
Issue them inside the running openvoxserver container and copy them
into `certs/`, where the compose file mounts them for the console.

### The openvoxdb client certificate

This is how the console authenticates to openvoxdb:

```sh
docker compose -f docker-compose.yml exec openvoxserver \
  puppetserver ca generate --certname console

docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/certs/console.pem certs/console-cert.pem
docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/private_keys/console.pem certs/console-key.pem
docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/certs/ca.pem certs/ca.pem
```

### The node transport certificate

Nodes check this certificate when they connect to the console, so it
must carry the exact name they dial: the host part of
`CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`.

```sh
docker compose -f docker-compose.yml exec openvoxserver \
  puppetserver ca generate --certname node-transport \
  --subject-alt-names console.example.com,node-transport

docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/certs/node-transport.pem certs/node-transport-cert.pem
docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/private_keys/node-transport.pem certs/node-transport-key.pem
```

Nodes authenticate back with their own certificates from the same CA,
which is why the console needs no other file for them.

<!-- include: rbac-key.md -->

Generate it straight into `certs/`:

```sh
openssl ecparam -genkey -name prime256v1 -noout -out certs/rbac-signing-key.pem
```

<!-- include: ca-client-intro.md -->

Issuing it writes to CA state that a running openvoxserver also writes,
so stop openvoxserver first and issue it from a one-off container using
the same volumes. Compose names volumes after the project directory, so
check the exact names first:

```sh
docker volume ls | grep openvoxserver
```

Then, using the two names it printed:

```sh
docker compose -f docker-compose.yml stop openvoxserver

docker run --rm --entrypoint "" \
  -v openvox-console_openvoxserver-ssl:/etc/puppetlabs/puppet/ssl \
  -v openvox-console_openvoxserver-ca:/etc/puppetlabs/puppetserver/ca \
  ghcr.io/avitacco/openvox-console-server:main \
  puppetserver ca generate --ca-client --force --certname console-ca-client

docker compose -f docker-compose.yml start openvoxserver
```

> [!WARNING]
> `--force` is required here. Before signing, the command checks that
> the server is really stopped, and inside a bare container it cannot
> tell, so without `--force` it prints "Could not determine whether
> Puppet Server is online" and exits having created nothing. Read its
> output rather than assuming it worked.

It reports writing its files under a long `.puppetlabs` path. Ignore
that: in the volumes they land in the usual place. Copy them out:

```sh
docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/certs/console-ca-client.pem certs/ca-client-cert.pem
docker compose -f docker-compose.yml cp \
  openvoxserver:/etc/puppetlabs/puppet/ssl/private_keys/console-ca-client.pem certs/ca-client-key.pem
```

Then tell the console where the CA is, in `.env`. Inside the compose
network the console reaches openvoxserver by its container name, which
is always in openvoxserver's certificate:

```sh
CONSOLE_CA_CLIENT_URL=https://openvoxserver:8140
```

Confirm the credential is accepted, from inside openvoxserver:

```sh
docker compose -f docker-compose.yml exec openvoxserver \
  curl -sS -o /dev/null -w 'HTTP %{http_code}\n' \
  --cert /etc/puppetlabs/puppet/ssl/certs/console-ca-client.pem \
  --key /etc/puppetlabs/puppet/ssl/private_keys/console-ca-client.pem \
  --cacert /etc/puppetlabs/puppet/ssl/certs/ca.pem \
  https://openvoxserver:8140/puppet-ca/v1/certificate_statuses/all
```

`HTTP 200` means the CA accepts it.

## Start everything else

```sh
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
```

openvoxdb requests its own certificate from the CA on first start. The
compose file signs exactly that one request automatically, through an
allow list holding only `openvoxdb`; nodes are never signed that way.
openvoxdb then waits for its database, and the console waits for both.
Watch them come up:

```sh
docker compose -f docker-compose.yml ps
```

Every service reaches `healthy` within a few minutes.

### If openvoxdb never becomes healthy

openvoxdb gives up if its certificate request is not signed within two
minutes, and when it does, it writes the failure into its own certificate
file. Every restart after that sees a certificate file, skips enrolling,
and fails with `No certs found in 'ssl-cert' file`. It does not recover
on its own. Reset its identity rather than restarting it:

```sh
docker compose -f docker-compose.yml rm -sf openvoxdb
docker volume rm openvox-console_openvoxdb-ssl
docker compose -f docker-compose.yml exec openvoxserver \
  puppetserver ca clean --certname openvoxdb
docker compose -f docker-compose.yml up -d openvoxdb
```

The volume name follows the project directory's name; check it with
`docker volume ls`. Skipping the `ca clean` leaves the CA holding a signed
certificate for `openvoxdb` with no key to match, and the fresh start
fails with `No keypair on disk and CA already has signed certificate`.

<!-- include: first-start.md -->

<!-- include: enc-token.md -->

### Give openvoxserver the token

The openvoxserver image already contains `enc-bridge` and the Puppet
configuration that uses it, so all it needs is the token. Write it to the
secret file you created empty earlier:

```sh
printf '%s' '<the enc:read service token>' > secrets/enc_bridge_token
```

openvoxserver does not run as root inside its container, and Compose
mounts secret files with their ownership on the host. A file readable only
by your own user is therefore unreadable to openvoxserver, and
classification fails. Give group 0 read access and protect the directory
instead:

```sh
sudo chgrp 0 secrets/enc_bridge_token
chmod 640 secrets/enc_bridge_token
chmod 700 secrets
```

Recreate openvoxserver so it reads the new token:

```sh
docker compose -f docker-compose.yml up -d --force-recreate openvoxserver
```

### Check classification

Ask enc-bridge directly, as openvoxserver runs it. This checks the
program, the token, the file permissions and the console's answer in one
command:

```sh
docker compose -f docker-compose.yml exec -u puppet:0 openvoxserver \
  /usr/local/bin/enc-bridge <a certname>
```

<!-- include: enc-verify.md -->

openvoxserver caches environments, so after adding Puppet code, flush its
cache or the same error persists:

```sh
docker compose -f docker-compose.yml exec openvoxserver sh -c \
  'curl -ksS -X DELETE \
     --cert /etc/puppetlabs/puppet/ssl/certs/openvoxserver.pem \
     --key /etc/puppetlabs/puppet/ssl/private_keys/openvoxserver.pem \
     https://127.0.0.1:8140/puppet-admin-api/v1/environment-cache'
```

## Upgrade later

To move to a newer console, change `CONSOLE_IMAGE_TAG` (or pull again if
you follow `main`) and bring the stack up again. Compose recreates only
what changed:

```sh
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d
```

<!-- include: checklist.md -->
