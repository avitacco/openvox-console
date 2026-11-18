package nodeagent

import "testing"

func requiredAgentEnv(extra map[string]string) func(string) string {
	env := map[string]string{
		"NODE_AGENT_TRANSPORT_ADDR": "console.example.com:7422",
		"NODE_AGENT_CERT_FILE":      "/etc/puppetlabs/puppet/ssl/certs/node.pem",
		"NODE_AGENT_KEY_FILE":       "/etc/puppetlabs/puppet/ssl/private_keys/node.pem",
		"NODE_AGENT_CA_FILE":        "/etc/puppetlabs/puppet/ssl/certs/ca.pem",
	}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestLoadConfig_TaskSettingsDefault(t *testing.T) {
	cfg, err := loadConfig(requiredAgentEnv(nil), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PuppetServer != "" {
		t.Errorf("PuppetServer = %q, want empty (ask Puppet)", cfg.PuppetServer)
	}
	if cfg.TaskCacheDir != "/opt/puppetlabs/node-agent/task-cache" {
		t.Errorf("TaskCacheDir = %q", cfg.TaskCacheDir)
	}
}

func TestLoadConfig_TaskCacheDefaultOnWindows(t *testing.T) {
	cfg, err := loadConfig(requiredAgentEnv(map[string]string{"ProgramData": `D:\ProgramData`}), "windows")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskCacheDir != `D:\ProgramData\PuppetLabs\node-agent\task-cache` {
		t.Errorf("TaskCacheDir = %q", cfg.TaskCacheDir)
	}
	cfg, _ = loadConfig(requiredAgentEnv(nil), "windows")
	if cfg.TaskCacheDir != `C:\ProgramData\PuppetLabs\node-agent\task-cache` {
		t.Errorf("TaskCacheDir without ProgramData = %q", cfg.TaskCacheDir)
	}
}

func TestLoadConfig_TaskSettingsExplicit(t *testing.T) {
	cfg, err := loadConfig(requiredAgentEnv(map[string]string{
		"NODE_AGENT_PUPPET_SERVER":  "compiler01.example.com:8140",
		"NODE_AGENT_TASK_CACHE_DIR": "/var/cache/tasks",
	}), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PuppetServer != "compiler01.example.com:8140" || cfg.TaskCacheDir != "/var/cache/tasks" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	if _, err := loadConfig(func(string) string { return "" }, "linux"); err == nil {
		t.Error("loadConfig succeeded with nothing set")
	}
}
