#!/opt/puppetlabs/puppet/bin/ruby
require 'json'
params = JSON.parse(STDIN.read)
require File.join(params['_installdir'], 'task_fixture', 'lib', 'task_fixture', 'helper.rb')
puts({ 'shout' => TaskFixture.shout(params['word']) }.to_json)
