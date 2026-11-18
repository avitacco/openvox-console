package nodeagent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func configRunner(stdout string, exitCode int, err error, gotArgs *[]string) CommandRunner {
	return func(_ context.Context, _ string, args ...string) (string, string, int, error) {
		if gotArgs != nil {
			*gotArgs = args
		}
		return stdout, "boom", exitCode, err
	}
}

func TestPuppetServerFromConfig(t *testing.T) {
	for name, tc := range map[string]struct {
		stdout string
		want   string
	}{
		// The real output of openvoxserver's own agent configuration.
		"server only":                    {"server = openvoxserver\nserver_list = \nserverport = 8140\n", "openvoxserver:8140"},
		"custom port":                    {"server = puppet.example.com\nserver_list = \nserverport = 8141\n", "puppet.example.com:8141"},
		"server_list wins":               {"server = puppet\nserver_list = compiler01.example.com:8140,compiler02.example.com\nserverport = 8140\n", "compiler01.example.com:8140"},
		"server_list entry without port": {"server = puppet\nserver_list = compiler01.example.com\nserverport = 8150\n", "compiler01.example.com:8150"},
	} {
		t.Run(name, func(t *testing.T) {
			var args []string
			got, err := PuppetServerFromConfig(context.Background(), configRunner(tc.stdout, 0, nil, &args), "puppet")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if strings.Join(args, " ") != "config print server_list server serverport --section agent" {
				t.Errorf("args = %v", args)
			}
		})
	}
}

func TestPuppetServerFromConfigFailures(t *testing.T) {
	for name, runner := range map[string]CommandRunner{
		"puppet missing": configRunner("", 0, errors.New("not found"), nil),
		"puppet fails":   configRunner("", 1, nil, nil),
		"nothing set":    configRunner("server = \nserver_list = \nserverport = 8140\n", 0, nil, nil),
	} {
		if _, err := PuppetServerFromConfig(context.Background(), runner, "puppet"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
