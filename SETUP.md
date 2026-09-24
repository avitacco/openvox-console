# Setting up OpenVox Console

The setup guides now live on the project site, alongside the rest of the
user documentation, in every language the site is published in:

- **Plan a deployment** - what you are building, and the names to settle
  first.
- **Install with containers** - the whole stack with Docker Compose,
  from an empty host to a console ready for nodes.
- **Install from packages and binaries** - openvoxserver, openvoxdb and
  Postgres from packages, and the console under systemd.
- **Add nodes** - enrolling nodes on Linux, macOS and Windows.

Open **Guides** from any page of the site. The same guides are in this
repository as Markdown, and read fine here too:

- [`marketing/guides/install/plan.md`](marketing/guides/install/plan.md)
- [`marketing/guides/install/containers.md`](marketing/guides/install/containers.md)
- [`marketing/guides/install/packages.md`](marketing/guides/install/packages.md)
- [`marketing/guides/nodes/add.md`](marketing/guides/nodes/add.md)

Shared passages, such as issuing the console's signing key, are in
[`marketing/guides/_partials/`](marketing/guides/_partials/) and included
where each guide says `<!-- include: ... -->`.

For a throwaway local development stack, see `README.md` instead.
