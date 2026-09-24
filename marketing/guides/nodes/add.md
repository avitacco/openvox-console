---
title: Add nodes
summary: Enrol a node with one script on Linux, macOS or Windows, sign its certificate, and see it connect and run.
order: 1
---

Adding a node is one script, run once on the node. It installs the
OpenVox agent, enrols the node with the CA if it has no certificate yet,
installs the console's node agent from the console's own package
repository, and starts it as a service. It is safe to run again.

## Before you start

The console has to know two addresses a node will use, both set when you
installed it:

- `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`, the address nodes dial to keep
  their connection to the console open. Its host must be in the node
  transport certificate.
- `CONSOLE_PUPPET_SERVER_PUBLIC_ADDR`, the Puppet server a node enrols
  against. It must be a name in openvoxserver's certificate. If it is
  unset, the script leaves the node's Puppet server configuration alone,
  which suits nodes already pointed at a server some other way.

Each node must be able to reach the console's base URL, openvoxserver on
port 8140, and the node transport address.

> [!NOTE]
> The published console image already contains the node agent packages
> the script installs. If you built the console yourself, build them
> before the console, as the packages install guide describes, or every
> node install fails with a 404.

## Run the install script

On the node, as an administrator:

```sh tab="Linux"
curl -fsSL https://console.example.com/packages/install.sh | sudo bash
```

```sh tab="macOS"
curl -fsSL https://console.example.com/packages/install-macos.sh | sudo bash
```

```powershell tab="Windows"
Invoke-WebRequest https://console.example.com/packages/install.ps1 -OutFile install.ps1
.\install.ps1
```

On Windows, run PowerShell as Administrator.

> [!NOTE]
> The macOS and Windows scripts are checked by the project's tests but
> have not yet been run end to end on real macOS and Windows hosts, as
> the Linux script has. Report anything that does not go as described
> here.

## Sign the node's certificate

If the CA signs requests automatically, the script finishes on its own.
If not, it waits for up to five minutes, checking every 15 seconds. Sign
the request while it waits and the same run carries on to the end:

1. Open the console's **Nodes** page. The node is listed with a pending
   certificate request.
2. Choose **Sign**.

Signing from the console needs the certificate-admin credential, and a
user holding the `nodes:certs:manage` permission; see
[Manage node certificates](certificates.html). You can also sign on the
CA host itself:

```sh
sudo /opt/puppetlabs/bin/puppetserver ca sign --certname <node certname>
```

The script ends in one of three ways:

| What happened | What you see |
| --- | --- |
| The request was signed while it waited | It carries on and finishes the install |
| The wait ran out with the request still unsigned | It stops, naming the Nodes page to sign it on; sign it, then run the script again |
| It could not reach the CA | It stops, reporting a connection or TLS problem - not a pending request |

> [!TIP]
> An unreachable CA is only reported once the full wait has passed,
> because the agent keeps retrying in case the problem is brief. If you
> see a connection error, check name resolution and port 8140 rather than
> looking for a request to sign.

## See the node connect

On the **Nodes** page, the node shows as connected, with a signed
certificate.

A node that connects before it has ever reported to openvoxdb would
otherwise sit there empty until its next scheduled Puppet run, so the
console starts its first run for it straight away. That run appears on
the **Jobs** page like any other, recorded as started by
`system:initial-run`. Once it finishes, the node's page shows its facts,
its reports and the classes applied to it.

## Report the node's packages (optional)

Package inventory is off for each node until you turn it on. On the
node's page, in its **Packages** section, switch reporting on. The
console tells the node to add the package fact and runs Puppet straight
away, so the inventory appears within that run rather than at the next
scheduled one. Switching it off removes the fact the same way.

The switch needs the `orchestrator:run` permission, and works only while
the node is connected: it acts on the node there and then, rather than
queueing a change for later.
