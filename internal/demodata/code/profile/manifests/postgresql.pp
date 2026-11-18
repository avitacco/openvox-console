# @summary The database tier's database server.
#
# A stand-in for the development stack and the demo data: it declares the
# parameters the demo node groups set and manages nothing, so a node
# classified with it compiles a catalog and reports no changes.
#
# @param max_connections
#   The most client connections the database accepts.
class profile::postgresql (
  Integer[1] $max_connections = 100,
) {
}
