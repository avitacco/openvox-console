package nodeagent

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// PuppetServerFromConfig asks the node's own Puppet configuration which
// server its agent uses, so task files come from the same server - often
// a compiler - that serves the node its catalog (design decision 4 in
// add-task-execution). The first server_list entry wins, as it does for
// the agent; otherwise server and serverport.
//
// SRV-record discovery (use_srv_records) is not resolved here: such a
// node sets NODE_AGENT_PUPPET_SERVER instead.
func PuppetServerFromConfig(ctx context.Context, runner CommandRunner, puppetBin string) (string, error) {
	stdout, stderr, exitCode, err := runner(ctx, puppetBin, "config", "print", "server_list", "server", "serverport", "--section", "agent")
	if err != nil {
		return "", fmt.Errorf("ask puppet for its server: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("ask puppet for its server: puppet config print exited %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	// With several settings, `puppet config print` writes one
	// "name = value" line each.
	settings := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			settings[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	port := settings["serverport"]
	if port == "" {
		port = "8140"
	}
	if list := settings["server_list"]; list != "" {
		first := strings.TrimSpace(strings.Split(list, ",")[0])
		if first != "" {
			if _, _, err := net.SplitHostPort(first); err == nil {
				return first, nil
			}
			return net.JoinHostPort(first, port), nil
		}
	}
	if server := settings["server"]; server != "" {
		return net.JoinHostPort(server, port), nil
	}
	return "", fmt.Errorf("puppet reports no server or server_list; set NODE_AGENT_PUPPET_SERVER")
}
