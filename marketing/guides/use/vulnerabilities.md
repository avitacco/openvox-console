---
title: Track vulnerabilities
summary: Set up vulnerability providers, read the findings for the fleet, and know what each provider can and cannot see.
order: 5
---

The console reports which nodes are affected by which known
vulnerabilities, merged from one or more **providers**. It matches each
provider's data against the packages each node reports, so nodes need
package reporting switched on: see [Add nodes](add.html).

Seeing findings needs `vulnerabilities:read`; configuring providers needs
`vulnerabilities:manage`. Neither is implied by `nodes:read`: findings are
more sensitive than an inventory.

## Choose a provider

| Provider | Data from | Credentials |
| --- | --- | --- |
| OSV | the public OSV database, or your own mirror of it | none |
| Tenable | your Tenable tenant | API keys |

Any number of providers can run at once, including two OSV providers with
different data locations. A vulnerability reported by several shows once,
listing each provider's own severity and fixed version.

Nothing leaves the console until you enable a provider.

## Prepare for credentials

A provider with credentials needs a key to seal them with in the
database. Generate one, and name it in `CONSOLE_SECRETS_KEY_FILE`:

```sh
openssl rand -base64 32 > secrets.key
chmod 600 secrets.key
```

Without it, OSV works, and a provider needing credentials is refused with
an error naming the setting.

> [!CAUTION]
> Back the key up with your database backups, but store it apart from
> them. Losing it loses no findings, but every stored credential becomes
> unreadable, and those providers fail until their credentials are
> entered again under a new key.

## Add a provider

1. Open **Vulnerabilities**, then **Providers**.
2. Add a provider of the type you want, and fill in its settings and any
   credentials. The console never shows a stored credential again; leave
   a credential field empty when editing to keep the stored one.
3. Enable it.

It syncs straight away and then on a schedule, hourly by default for OSV.
Findings refresh when a provider syncs, not at every Puppet run.

### What OSV downloads

OSV downloads data only for the distributions your fleet runs, over
HTTPS from `storage.googleapis.com` by default. The first sync of a
distribution downloads its whole archive - around 70 MB for Debian and
700 MB for Ubuntu - and later syncs only what changed.

### Air-gapped sites

Point an OSV provider's **Data location** at an internal mirror of the
OSV bucket, laid out the same way for each distribution: its `all.zip`,
its `modified_id.csv`, and a JSON file per changed record. A plain static
web server is enough, and copying each distribution's directory is
enough to fill it:

```sh
gsutil -m rsync -r gs://osv-vulnerabilities/Debian ./Debian
```

Directory names contain spaces, such as `Rocky Linux` and `Red Hat`.

### Tenable

A Tenable provider imports every open finding, then only changes. Each
Tenable asset is matched to a node by its names against each node's
certname and reported FQDN. An asset matching no node, or several, is
skipped and counted as unmatched; a growing count usually means Tenable
sees different names than your certnames.

## Read the findings

The **Vulnerabilities** page lists findings across the fleet, most severe
first, with how many nodes each affects. It shows findings with a fix
available by default, which is what patching would change; set **Fix** to
**All** to include those with no fix released.

A node that no enabled provider could assess is shown as **not
assessed**, with each provider's reason, never as clean:

| Reason | Means |
| --- | --- |
| unsupported OS | OSV covers Debian, Ubuntu, AlmaLinux, Rocky Linux and RHEL 7 to 10 |
| no package data | the node reports no packages; switch reporting on for it |
| agent upgrade required | the node's package data is too old to match reliably; upgrading its node agent fixes it at its next Puppet run |
| not seen by Tenable | no Tenable asset matched the node |
| not yet synced | the provider has not finished a sync |

## What to expect from OSV

- On Debian and Ubuntu, many vulnerabilities the distribution rates as
  negligible, or has decided not to fix, still appear, as findings with
  no fix released. That is why the list starts at "fix available".
- RHEL nodes are matched against the main repositories only. Nodes using
  extended-support or add-on repositories may show findings they do not
  have.
- AlmaLinux publishes no severity, so its findings show **Unknown**.

> [!TIP]
> Findings against old kernels and other packages you removed long ago
> usually come from packages removed without purging, whose
> configuration files remain. Purge them on the node with
> `apt purge '~c'`; the findings close at the provider's next sync.
