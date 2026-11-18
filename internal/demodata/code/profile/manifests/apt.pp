# @summary Package sources on Debian-family nodes.
#
# A stand-in for the development stack and the demo data: it declares the
# parameters the demo node groups set and manages nothing, so a node
# classified with it compiles a catalog and reports no changes.
#
# @param purge_sources
#   Whether to remove package sources this profile does not manage.
class profile::apt (
  Boolean $purge_sources = false,
) {
}
