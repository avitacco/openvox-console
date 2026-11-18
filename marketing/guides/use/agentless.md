---
title: Run tasks on hosts without an agent
summary: Register hosts the console reaches over SSH, pin their host keys, check them, and run tasks on them like any other node.
order: 9
---

Some hosts cannot or should not run the node agent: appliances, locked-down
servers, machines not yet enrolled. The console can reach a Linux host over
**SSH** instead and run [tasks](run-jobs.html) on it. Such a host is an
**agentless target**.

What an agentless target can do is deliberately narrow:

- **Tasks run on it**, on their own, in a [plan](run-plans.html) or on a
  [schedule](schedule-jobs.html), with the same permissions, node groups and
  task allowlists as any node.
- **Puppet runs and patch runs do not.** They need the agent. A request that
  names an agentless target for one is refused whole, naming the target, and
  nothing is sent to anyone.
- It has **no facts or reports**, so a node group's rule, which matches on
  facts, cannot include it. A group includes it by **pin**.

## Add a target

Open **Nodes**, then **Agentless targets**, then **Add target**. This needs
`agentless:manage`. Give:

- a **name**, which is how the target appears in jobs and node groups. It
  cannot be the name of a node that exists, or of another target;
- the **address** and **port** (22 unless you say otherwise);
- the **login**: a user, and a **private key** (with its passphrase if it has
  one), a **password**, or both. Or choose a **shared credential**: one login
  several targets use. A target's own login wins over a shared one;
- optionally a user to **run tasks as**. Tasks then run through
  `sudo -n -u <user>`, so `sudo` must not ask for a password. Password `sudo`
  is not supported.

The key, passphrase and password are stored **sealed** with the console's
secrets key (`CONSOLE_SECRETS_KEY_FILE`; without it a target with credentials
cannot be added). They are never returned by the console, written to its logs
or audit entries, or put in a job. You can set or replace one but never read
it: leave the fields blank when you edit to keep the stored ones. A key is
checked when you save it, and a passphrase that does not open it is refused.

A target must be pinned into a node group to be seen by anyone limited to
that group. A user limited to some node groups can add and manage targets
only inside their groups, and cannot use shared credentials at all, since a
shared credential belongs to no node.

## Trust its host key

Before the console sends a login to a host it must know it is the right one.

1. **Check** the target. The console connects and reads the host's key
   **without logging in**, and shows its fingerprint.
2. Compare that fingerprint with the host's own, for example
   `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the host, and
   choose **Trust this key**.
3. Check again: now the console logs in, and reports who it logged in as
   (and as whom tasks will run, if you set a run-as user).

Until a key is trusted, a task on the target fails as "not trusted" and
nothing is run. If a trusted target later presents a **different key**, the
connection is refused before any login is sent, and the target shows
**Host key changed** with both fingerprints. Replace the pin only if you know
the host was rebuilt or its key rotated: that is a separate, audited action.
Changing a target's address or port clears its pin, since a pin vouches for a
host, not a name.

A failed check says which stage failed: connecting, the host key,
authentication, becoming the run-as user, or running a command.

## Run a task on one

On the Nodes page select targets with any other nodes and choose **Run
task**, or name them in a task, plan or schedule. The job shows each target's
own result.

For each target the console downloads the task's files from the Puppet
server, verifies their checksums, then connects, creates a private directory
on the host (`mktemp -d`, mode 700), copies the files into it over the SSH
session, runs the task and removes the directory, whether the task succeeded,
failed or timed out. Nothing needs to be installed on the host beyond `sh`,
`cat`, `mktemp` and (for a run-as user) `sudo`.

- Task **parameters never appear on a command line**, where any user of the
  host could read them. They reach the task on standard input or in its
  environment, as the task's metadata asks, and are never written to disk.
  What a task prints is recorded like any task's output.
- A task whose implementations all need the Puppet agent (`puppet-agent`)
  cannot run: the target provides a shell only.
- A task is limited by its **timeout**. When it runs out, the task is stopped
  and the target fails with that reason. Connecting is limited to 15 seconds.
- A target runs **one task at a time**; a second is refused as busy. At most
  20 SSH sessions run across the console at once, and the rest wait.
- A `/tmp` mounted `noexec` stops a task from running there. The failure says
  so; make a directory available or remount.

## Where it is audited

Adding, changing, deleting and checking a target, trusting or replacing a
host key, and every shared credential change are audit entries naming who
and what. They name which secrets were set, never their values. Jobs are
audited as always.

## Not covered

Windows and WinRM, jump hosts, SSH certificates, Puppet runs, patch scans
and patch runs, collecting facts, and Puppet-language plans.
