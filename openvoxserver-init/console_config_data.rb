# frozen_string_literal: true

require 'json'

# Hiera data_hash backend that serves a node's configuration data from
# the OpenVox Console (node groups' "Configuration data").
#
# It is wired in by openvoxserver-init/30-configure-config-data.sh, which
# copies this file into the openvox gem's own puppet/functions directory
# (the only place the server loads a global-layer function from) and adds
# it to the global hiera.yaml, ahead of the environment's data. Console
# data therefore wins over the control repo's for a key set in both, and a
# key the console does not set is found in the control repo as before.
#
# The data comes from `enc-bridge --data <certname>`: the same program,
# found through the same `external_nodes` setting, with the same console
# address and token as classification. Running it rather than calling the
# console from here is deliberate: openvoxserver does not give Ruby code
# its own environment variables (ENC_BRIDGE_URL and the token are not
# visible here), but it does give them to the programs it runs. One run per
# catalog compile.
#
# A console that cannot answer fails the compile. Returning nothing would
# compile a catalog with the wrong values and apply it.
Puppet::Functions.create_function(:console_config_data) do
  dispatch :console_config_data do
    param 'Hash', :options
    param 'Puppet::LookupContext', :context
  end

  DEFAULT_BRIDGE = '/usr/local/bin/enc-bridge'

  def console_config_data(_options, _context)
    trusted = closure_scope['trusted']
    certname = trusted['certname'] if trusted.is_a?(Hash)
    if certname.nil? || certname.empty?
      raise Puppet::Error, 'console_config_data: the node has no trusted certname'
    end

    bridge = bridge_path
    output = begin
      Puppet::Util::Execution.execute([bridge, '--data', certname], failonfail: true, combine: true).to_s
    rescue Puppet::ExecutionFailure, SystemCallError => e
      raise Puppet::Error,
            "console_config_data: could not get the configuration data of #{certname} from the console " \
            "(#{bridge} --data): #{e.message}"
    end

    data = begin
      JSON.parse(output)
    rescue JSON::ParserError => e
      raise Puppet::Error, "console_config_data: #{bridge} --data did not print JSON: #{e.message}"
    end
    raise Puppet::Error, "console_config_data: #{bridge} --data did not print a JSON object" unless data.is_a?(Hash)

    data
  end

  # The program the ENC runs, from puppet.conf's external_nodes (which may
  # carry arguments), or where the image installs it.
  def bridge_path
    configured = Puppet[:external_nodes].to_s.split.first
    return DEFAULT_BRIDGE if configured.nil? || configured.empty? || configured == 'none'

    configured
  end
end
