---
title: Manage node certificates
summary: Sign, revoke and clean node certificates from the console, and what each does to a connected node.
order: 2
---

Every node's identity is a certificate from openvoxserver's CA. With the
certificate-admin credential configured, the console's **Nodes** page
shows each certificate's state and lets you act on it.

## Turn certificate management on

Certificate management needs the certificate-admin credential, issued
during installation. Both install guides cover issuing it. The console
uses it when all four settings are present:

| Setting | What it is |
| --- | --- |
| `CONSOLE_CA_CLIENT_URL` | openvoxserver's address, as the console reaches it |
| `CONSOLE_CA_CLIENT_CERT_FILE` | the credential's certificate |
| `CONSOLE_CA_CLIENT_KEY_FILE` | its private key |
| `CONSOLE_CA_CLIENT_CA_FILE` | the CA bundle, to verify openvoxserver |

With any one unset, every node's certificate state reads `unknown` and
the rest of the console works as usual.

> [!WARNING]
> The credential is full CA admin: anything that can use it can sign,
> revoke and clean *any* node's certificate. openvoxserver has no
> narrower variant. Keep its key readable only by the console.

## Decide who may act on certificates

Two permissions are involved, and they are deliberately far apart:

| Permission | Allows |
| --- | --- |
| `nodes:read` | seeing each node and its certificate state |
| `nodes:certs:manage` | signing, revoking and cleaning certificates |

Granting `nodes:certs:manage` is equivalent to giving shell access to the
CA: there is no way to allow signing but not revoking, or one node but
not another. Give it to specific trusted operators, not to everyone who
needs to see node status.

## Sign, revoke or clean

On the **Nodes** page, each node's certificate state is one of
`requested`, `signed`, `revoked` or `unknown`, and the actions offered
follow from it:

| Action | Available when | What it does |
| --- | --- | --- |
| **Sign** | `requested` | Issues the node's certificate, so it can authenticate |
| **Revoke** | `signed` | Stops the certificate being accepted anywhere, for good |
| **Clean** | any known state | Revokes the certificate if needed, then removes the CA's record of the certname entirely |

Clean is what to use before a node enrols again under the same name: once
the record is gone, the certname can submit a fresh request.

An action the CA refuses, such as signing a certificate that is not
waiting to be signed, fails with the reason and changes nothing. Each
completed action is written to the audit log as
`node.certificate.signed`, `node.certificate.revoked` or
`node.certificate.cleaned`.

## What revocation does to a connected node

A node revoked or cleaned through the console is cut off at once. Every
console instance is told, and whichever one holds the node's connection
closes it. Each instance also fetches the CA's current revocation list
first, so the node cannot simply reconnect.

Certificates revoked outside the console, with
`puppetserver ca revoke` on the CA host, are caught too: each instance
re-reads the revocation list every minute, and drops any live connection
it now revokes.

The revocation list comes from one of two places:

- the file named by `CONSOLE_NODE_TRANSPORT_CRL_FILE`, typically the
  console host's own Puppet agent's `crl.pem`, which the agent keeps
  current. The console refuses to start if this is set but unreadable;
- otherwise, the CA itself, through the certificate-admin credential.

With neither, the console logs a warning at startup: a revoked node
certificate can then still connect.

## Infrastructure certificates

The Nodes page lists every certname the CA knows, which includes the
console's own certificates and those of the servers it talks to:
`console`, `node-transport`, `console-ca-client`, `openvoxdb` and
`openvoxserver` in a default install. These are recognised automatically
and hidden by default. **Show infrastructure certs** reveals them,
marked, and revoking or cleaning one asks for confirmation naming what
would break.

## Retire the certificate-admin credential

Because it is full CA admin, clean the credential as soon as it is no
longer needed, for example when a console instance is decommissioned. On
the CA host:

```sh
sudo /opt/puppetlabs/bin/puppetserver ca clean --certname console-ca-client
```

Then remove the four `CONSOLE_CA_CLIENT_*` settings from every console
still configured with it. A console still holding the cleaned credential
simply reports every certificate as `unknown`; nothing else stops working.
