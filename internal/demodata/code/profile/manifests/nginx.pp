# @summary The web tier's web server.
#
# A stand-in for the development stack and the demo data: it declares the
# parameters the demo node groups set and manages nothing, so a node
# classified with it compiles a catalog and reports no changes.
#
# @param worker_processes
#   How many worker processes the web server runs.
class profile::nginx (
  Integer[1] $worker_processes = 2,
) {
}
