### The certificate-admin credential (optional)

This credential is what lets the **Nodes** page show certificate status,
and sign, revoke and clean certificates. Without it the console lists
only nodes that have already connected or reported. A node waiting for
its certificate to be signed does not appear at all, because the CA is
the only part that knows it exists.

> [!WARNING]
> This credential is full CA admin. openvoxserver gates every CA
> endpoint behind one authorization extension, so a certificate that can
> read statuses can also sign, revoke and clean *any* node's certificate.
> There is no read-only variant. Keep it only where the console runs, and
> read [Manage node certificates](certificates.html) before issuing it.
