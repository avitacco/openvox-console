---
title: Plan a deployment
summary: What you are building, the names to settle before you start, and which install guide to follow.
order: 1
---

## What you are building

A working OpenVox Console deployment has five parts:

| Component | What it does | Default port |
| --- | --- | --- |
| **openvoxserver** | Compiles catalogs, and is the **certificate authority** every other part's identity comes from | 8140 |
| **openvoxdb** | Stores facts, reports and package inventory; the console queries it | 8081 |
| **openvoxdb's Postgres** | openvoxdb's own storage | 5432 |
| **The console's Postgres** | The console's own storage: users, roles, groups, jobs and audit | 5432 |
| **The console** | The web interface and API, plus the node transport managed nodes connect to | 8080, 8142 |

The two Postgres databases are separate. They can share a server, but the
console and openvoxdb never share a database.

## Two things that cause most setup failures

1. **Everything is authenticated by certificates from one CA**, the one
   inside openvoxserver. The console holds several separate identities
   from it: a client certificate for openvoxdb, a server certificate for
   the node transport, and optionally a certificate-admin credential. So
   openvoxserver always comes first.
2. **Names matter more than addresses.** A certificate is valid for the
   names it was issued with. Decide up front which name each part will
   be reached by, from every side that reaches it, and put those names in
   its certificate. Adding a name later means reissuing the certificate.

## Decide these before you start

| Decision | Used for | Example |
| --- | --- | --- |
| openvoxserver's name, as nodes reach it | Enrolling nodes | `puppet.example.com` |
| openvoxserver's name, as the console reaches it | Certificate management from the console | the same, or an internal name |
| The console's base URL | Install script downloads and links | `https://console.example.com` |
| The node transport address, as nodes dial it | The connection nodes keep open to the console | `console.example.com:8142` |

If the console reaches openvoxserver by a different name than nodes do,
**both** names must be in openvoxserver's certificate.

> [!WARNING]
> Port 8080 collides if you run the console on the same host as
> openvoxdb, which also listens on 8080. The console then fails to start
> with "address already in use". On a shared host, move the console with
> `CONSOLE_HTTP_ADDR=:8088` and change `CONSOLE_BASE_URL` to match.

## Choose an install guide

Both guides end at the same place: a console serving classification to
openvoxserver, ready for nodes.

- [Install with containers](containers.html) runs every part with Docker
  Compose from the project's published images. It is the quickest route,
  and the one to choose unless you have a reason not to.
- [Install from packages and binaries](packages.html) installs
  openvoxserver, openvoxdb and Postgres from the OpenVox and distribution
  packages, and runs the console as a systemd service.

The parts interoperate freely, so you can also mix the two, for example
a packaged openvoxserver with a containerised console.
