---
title: Keep node agents current
summary: How the console upgrades the node agent on your nodes by itself, how to see which nodes are behind, and the one-time step for nodes installed before this existed.
order: 4
---

The node agent is the small service the install script puts on each
node. It holds the node's connection to the console and carries out the
runs and tasks you start. It is built with the console and ships inside
it, so a newer console usually brings a newer node agent.

The console upgrades the node agent on your nodes for you. When a node
connects, the console asks which build of the agent it runs. If that
build is older than the one the console ships, the console tells the
node to upgrade, and the node downloads the new build from the console,
checks it, installs it and restarts onto it. You do nothing on the node.

## What you see

- **Nodes page.** A node whose agent is behind is marked
  **Client out of date**.
- **A node's page.** Shows the agent's version and when it was built,
  whether it is current, and what became of the last upgrade attempt.
- **Dashboard.** Under **Fleet status**, how many node agents are
  current, how many are out of date, how many upgrades have failed, and
  whether automatic upgrades are on.

Starting an upgrade yourself, from a node's page, needs `nodes:manage`
on a role whose node groups include the node. The upgrades the console
starts by itself are not limited by anyone's node groups.

A node that has not connected since the console was upgraded shows as
not yet reported, not as out of date. It reports the next time it
connects.

## Nodes installed before automatic upgrades existed

A node agent from before this feature cannot upgrade itself, because it
does not understand the request. The console shows such a node as out of
date, with a note that the install script has to be run again. Run it on
the node once, exactly as you did to [add the node](add.html):

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

From then on the node keeps itself current. The dashboard counts the
nodes still waiting for this, so you can tell when you have done them
all.

If you manage the Linux package with Puppet instead, `ensure => latest`
on the `node-agent-client` package from the console's repository does
the same job.

## What an upgrade does and does not do

- **It never interrupts work.** A node that is in the middle of a run or
  a task is left alone and tried again a minute later.
- **It is checked before it is installed.** The console sends the
  expected checksum of the download over the node's authenticated
  connection. The node refuses anything that does not match, and starts
  the new agent once to confirm it works before replacing the old one.
  If either check fails, the node keeps the agent it has.
- **It goes a few nodes at a time.** At most 20 nodes upgrade at once,
  however many are behind.
- **It only goes forwards.** A node running a newer agent than the
  console ships is left alone. This is what you see part-way through
  upgrading a cluster of console instances, or after rolling the console
  back.
- **On Linux it goes through the package manager.** The node installs
  the console's `.deb` or `.rpm`, so `dpkg` and `rpm` show the version
  that is really installed.

> [!NOTE]
> Upgrades on macOS and Windows are covered by the project's tests but
> have not yet been run end to end on real macOS and Windows hosts.
> Report anything that does not go as described here.

## When an upgrade fails

The node's page shows the reason, in the node's own words: the download
failed, the checksum did not match, the package manager was busy. The
console tries again after five minutes, then waits twice as long each
time, and stops after five failures at the same build.

The most common cause is a node that cannot reach the console's base URL
(`CONSOLE_BASE_URL`), which is where it downloads the new agent from.
The install script used the same address, so check what has changed
since the node was installed.

Once you have fixed the cause, choose **Upgrade client** on the node's
page to try again straight away. This needs the `nodes:manage`
permission and a connected node. It works whether or not automatic
upgrades are on, and after the automatic attempts have stopped.

The console also starts again by itself when you upgrade it to a version
shipping a different node agent.

## Turning automatic upgrades off

To stop the console upgrading node agents by itself, set this on every
console instance and restart it:

```sh
CONSOLE_AGENT_AUTO_UPGRADE=false
```

The console still records each node's agent build and still shows which
nodes are out of date. **Upgrade client** on a node's page still works.

To exempt a single node instead, set `NODE_AGENT_ACCEPT_UPGRADES=false`
for the node agent on that node and restart it:

| Platform | Where to set it |
| --- | --- |
| Linux | Add the line to `/etc/node-agent-client/local.env`, then `sudo systemctl restart node-agent-client` |
| Windows | Set it as a system environment variable, then restart the `node-agent-client` service |
| macOS | Add it to `EnvironmentVariables` in `/Library/LaunchDaemons/org.voxpupuli.node-agent-client.plist`, then reload the job |

The node's page then says that upgrades are declined on that node, and
the console does not try. On macOS, running the install script again
rewrites that file, so set it again afterwards.
