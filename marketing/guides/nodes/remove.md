---
title: Remove a node
summary: Delete a node from the console, what that does in openvoxdb and the CA, and what it keeps.
order: 3
---

## Delete a node

On the **Nodes** page, choose **Delete** on the node, and confirm. This
needs the `nodes:manage` permission.

Deleting does two things, together:

- it **deactivates the node in openvoxdb**, so it no longer appears in
  the fleet;
- it **cleans the node's certificate from the CA**, the same as the
  **Clean** action, so the certname can never authenticate again and is
  free to enroll afresh.

Both halves are needed for the node to disappear from the page, which
lists nodes known to openvoxdb and nodes known to the CA. If the console
has no certificate-admin credential, only the openvoxdb half happens, and
the node may go on appearing through its certificate.

Before deleting a machine you are decommissioning, stop its node agent
and Puppet agent, or remove them, so it does not report or connect again.

## What deleting does not do

- **It does not erase the node's history straight away.** Its facts,
  catalogs and reports stay in openvoxdb until openvoxdb's own garbage
  collection removes them, on its own schedule.
- **There is no list of deleted nodes.** openvoxdb leaves deactivated
  nodes out of every listing, and the console keeps no separate record,
  so a deleted node is gone from the console's pages.

## Bring a node back

A deleted node's certificate has been cleaned, so bringing it back is the
same as adding a new node: run the install script on it again, and sign
its new request. See [Add nodes](add.html).
