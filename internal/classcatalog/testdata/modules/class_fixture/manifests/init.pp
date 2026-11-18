# Fixture for the console's class discovery: one parameter of each kind
# the group page gives its own input, one with no default (required),
# and one whose default is an expression rather than a literal.
class class_fixture (
  String $owner,
  Boolean $manage_service = true,
  Integer[1, 65535] $port = 8080,
  Float $ratio = 0.5,
  Enum['present', 'absent'] $ensure = 'present',
  Array[String] $servers = ['a.example.com', 'b.example.com'],
  Hash[String, Integer] $limits = { 'open_files' => 1024 },
  Optional[String] $comment = undef,
  Variant[String, Integer] $tag_or_id = 'web',
  Sensitive[String] $secret = Sensitive('changeme'),
  Stdlib::Absolutepath $config_dir = '/etc/class_fixture',
  String $greeting = "hello from ${facts['networking']['fqdn']}",
  $untyped = 'anything',
) {
  notify { "class_fixture for ${owner}": }
}
