node default {
  class { 'code_manager_test_marker': }
}

class code_manager_test_marker {
  file { '/tmp/enterprise-console-code-manager-test-marker':
    ensure  => file,
    content => "deployed by phase-5-code-manager\n",
  }
}
