# @summary The baseline every node gets.
#
# A stand-in for the development stack and the demo data: it declares the
# parameters the demo node groups set and manages nothing, so a node
# classified with it compiles a catalog and reports no changes.
#
# @param manage_ntp
#   Whether this profile manages time synchronisation.
class profile::base (
  Boolean $manage_ntp = true,
) {
}
