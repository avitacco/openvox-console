#!/bin/sh
# Lets the console read the classes each environment's code defines
# (GET /puppet/v3/environment_classes), which is what fills the class
# picker on a node group's page. The server's default auth.conf has no
# rule for that endpoint, so without this the request is refused and the
# console falls back to typed class names.
#
# Only the console's own certificate is allowed - the one it presents to
# this server, named "console" unless CLASS_DISCOVERY_CERTNAME says otherwise.
# auth.conf survives a restart but not a recreate, so this is idempotent
# and re-applies on every boot.
set -e

AUTH_CONF=/etc/puppetlabs/puppetserver/conf.d/auth.conf
RULE_NAME="openvox console class discovery"

if grep -q "$RULE_NAME" "$AUTH_CONF"; then
  exit 0
fi

AUTH_CONF="$AUTH_CONF" RULE_NAME="$RULE_NAME" \
ALLOW="${CLASS_DISCOVERY_CERTNAME:-console}" /opt/puppetlabs/puppet/bin/ruby <<'RUBY'
require 'hocon'
require 'hocon/parser/config_document_factory'
require 'hocon/config_value_factory'

auth_conf = ENV.fetch('AUTH_CONF')
rules = Hocon.load(auth_conf)['authorization']['rules']
rules << {
  'match-request' => {
    'path'   => '/puppet/v3/environment_classes',
    'type'   => 'path',
    'method' => 'get'
  },
  'allow'      => ENV.fetch('ALLOW'),
  'sort-order' => 500,
  'name'       => ENV.fetch('RULE_NAME')
}
doc = Hocon::Parser::ConfigDocumentFactory.parse_file(auth_conf)
doc = doc.set_config_value('authorization.rules', Hocon::ConfigValueFactory.from_any_ref(rules))
File.write(auth_conf, doc.render)
RUBY
