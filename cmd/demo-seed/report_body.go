package main

import (
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
)

// demoEvent is one resource-level change within a report, in the shape
// this file reasons about. It is deliberately not openvoxdb's wire shape:
// the same event has to appear in three places in a report (the resource
// list, the log lines, and the counters), and keeping one typed
// description of it is what stops those three disagreeing.
type demoEvent struct {
	ResourceType  string
	ResourceTitle string
	Property      string
	Status        string // "success", "failure" or "skipped"
	OldValue      any
	NewValue      any
	Message       string
	File          string
	Line          int
	At            time.Time
	Corrective    bool
}

// reportBody builds the resources, logs and metrics of a node's report.
//
// The wire shape here was confirmed against a live openvoxdb 8.15:
// "store report" version 8 requires a `resources` key - a list of
// resources each carrying its own nested `events` - and rejects the flat
// `resource_events` key outright ("{:resources missing-required-key,
// :resource_events disallowed-key}"). A rejected command still returns
// 200 from the submission endpoint, because that only acknowledges the
// queue, so getting this wrong fails silently until you read openvoxdb's
// own log.
func reportBody(node demodata.Node, end time.Time) (resources []map[string]any, logs []map[string]any, metrics []map[string]any) {
	events := resourceEvents(node, end)

	var changed, failed, skipped int
	for _, e := range events {
		switch e.Status {
		case "success":
			changed++
		case "failure":
			failed++
		case "skipped":
			skipped++
		}
	}

	return wireResources(events), reportLogs(node, end, events), reportMetrics(node, changed, failed, skipped)
}

// wireResources renders the events as openvoxdb's resources list. One
// resource per event is correct here because no demo node changes the
// same resource twice in a run.
func wireResources(events []demoEvent) []map[string]any {
	resources := make([]map[string]any, 0, len(events))
	for _, e := range events {
		event := map[string]any{
			"status":            e.Status,
			"timestamp":         timestamp(e.At),
			"property":          nullable(e.Property),
			"new_value":         e.NewValue,
			"old_value":         e.OldValue,
			"message":           nullable(e.Message),
			"corrective_change": e.Corrective,
		}
		resources = append(resources, map[string]any{
			"timestamp":      timestamp(e.At),
			"resource_type":  e.ResourceType,
			"resource_title": e.ResourceTitle,
			"file":           nullable(e.File),
			"line":           e.Line,
			// openvoxdb requires corrective_change on the resource as
			// well as on each of its events - confirmed live, same
			// silent-200-then-reject path as the resources key itself.
			"corrective_change": e.Corrective,
			"containment_path":  []string{"Stage[main]", e.ResourceType + "[" + e.ResourceTitle + "]"},
			"skipped":           e.Status == "skipped",
			"events":            []map[string]any{event},
		})
	}
	return resources
}

// nullable renders an empty string as JSON null, which is what openvoxdb
// expects for an absent optional field - an empty string is a value.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// resourceEvents are the per-resource changes a run made. An unchanged
// run legitimately has none - Puppet reports no events when nothing
// moved - which is why the Events tab of an unchanged report is empty in
// the product too.
func resourceEvents(node demodata.Node, end time.Time) []demoEvent {
	manifest := "/etc/puppetlabs/code/environments/" + node.Environment + "/site/profile/manifests/" + node.Role + ".pp"

	switch node.Status {
	case demodata.StatusUnchanged:
		return nil

	case demodata.StatusChanged:
		return []demoEvent{
			{
				ResourceType:  "File",
				ResourceTitle: configPathFor(node),
				Property:      "content",
				Status:        "success",
				OldValue:      "{sha256}0d3ee4f2c8bbd1e2e7ea1a5de3f6c1bb9b4b1a9e7dbb7b4b0c9e1f2a3b4c5d6e",
				NewValue:      "{sha256}7a1c2f9b4e8d0a6c3b5f1e9d2c4a8b6f0e3d7c1a5b9f2e4d8c6a0b3f7e1d5c9a",
				Message:       "content changed '{sha256}0d3ee4f2...' to '{sha256}7a1c2f9b...'",
				File:          manifest,
				Line:          42,
				At:            end.Add(-2 * time.Second),
				Corrective:    node.CorrectiveChange,
			},
			{
				ResourceType:  "Service",
				ResourceTitle: serviceFor(node.Role),
				Property:      "ensure",
				Status:        "success",
				OldValue:      "stopped",
				NewValue:      "running",
				Message:       "ensure changed 'stopped' to 'running'",
				File:          manifest,
				Line:          58,
				At:            end.Add(-1 * time.Second),
				Corrective:    node.CorrectiveChange,
			},
		}

	case demodata.StatusFailed:
		pkg := failingPackageFor(node)
		return []demoEvent{
			{
				ResourceType:  "Package",
				ResourceTitle: pkg,
				Property:      "ensure",
				Status:        "failure",
				OldValue:      "purged",
				NewValue:      "present",
				Message:       "Could not install package " + pkg + ": dependency resolution failed",
				File:          manifest,
				Line:          27,
				At:            end.Add(-2 * time.Second),
			},
			{
				ResourceType:  "Service",
				ResourceTitle: serviceFor(node.Role),
				Status:        "skipped",
				Message:       "Skipping because of failed dependencies",
				File:          manifest,
				Line:          58,
				At:            end.Add(-1 * time.Second),
			},
		}
	}
	return nil
}

// reportLogs are the run's log lines. Every report gets the standard
// opening and closing lines; a run with events gets a line per event, so
// the log and the event list tell the same story.
func reportLogs(node demodata.Node, end time.Time, events []demoEvent) []map[string]any {
	logs := []map[string]any{
		{
			"level":   "info",
			"message": "Applying configuration version '" + fmt.Sprintf("%d", end.Unix()) + "'",
			"source":  "Puppet",
			"tags":    []string{"info"},
			"time":    timestamp(end.Add(-runDuration(node))),
			"file":    nil,
			"line":    nil,
		},
	}

	for _, e := range events {
		level := "notice"
		switch e.Status {
		case "failure":
			level = "err"
		case "skipped":
			level = "warning"
		}
		logs = append(logs, map[string]any{
			"level":   level,
			"message": e.Message,
			"source":  fmt.Sprintf("/Stage[main]/%s/%s[%s]", classForRole(node.Role), e.ResourceType, e.ResourceTitle),
			"tags":    []string{node.Role},
			"time":    timestamp(e.At),
			"file":    nullable(e.File),
			"line":    e.Line,
		})
	}

	return append(logs, map[string]any{
		"level":   "notice",
		"message": fmt.Sprintf("Applied catalog in %.2f seconds", runDuration(node).Seconds()),
		"source":  "Puppet",
		"tags":    []string{"notice"},
		"time":    timestamp(end),
		"file":    nil,
		"line":    nil,
	})
}

// reportMetrics are the per-run counters the report page summarises. The
// resource totals are derived from the events actually present rather
// than written by hand, so the summary cannot disagree with the list
// below it.
func reportMetrics(node demodata.Node, changed, failed, skipped int) []map[string]any {
	total := resourceTotalFor(node.Role)
	duration := runDuration(node).Seconds()

	metric := func(category, name string, value any) map[string]any {
		return map[string]any{"category": category, "name": name, "value": value}
	}

	return []map[string]any{
		metric("resources", "total", total),
		metric("resources", "changed", changed),
		metric("resources", "failed", failed),
		metric("resources", "skipped", skipped),
		metric("resources", "out_of_sync", changed+failed),
		metric("resources", "restarted", 0),
		metric("resources", "scheduled", 0),
		metric("resources", "corrective_change", correctiveCount(node, changed)),
		metric("time", "total", duration),
		metric("time", "config_retrieval", duration*0.34),
		metric("time", "catalog_application", duration*0.61),
		metric("time", "file", duration*0.08),
		metric("time", "package", duration*0.22),
		metric("time", "service", duration*0.11),
		metric("events", "total", changed+failed),
		metric("events", "success", changed),
		metric("events", "failure", failed),
		metric("changes", "total", changed),
	}
}

func correctiveCount(node demodata.Node, changed int) int {
	if node.CorrectiveChange {
		return changed
	}
	return 0
}

func resourceTotalFor(role string) int {
	switch role {
	case "database":
		return 284
	case "web", "api":
		return 197
	case "monitoring":
		return 231
	case "ci":
		return 312
	default:
		return 156
	}
}

func configPathFor(node demodata.Node) string {
	if node.Platform.Family == "windows" {
		return `C:\ProgramData\app\config.ini`
	}
	switch node.Role {
	case "web", "api":
		return "/etc/nginx/conf.d/site.conf"
	case "database":
		return "/etc/postgresql/15/main/postgresql.conf"
	case "cache":
		return "/etc/redis/redis.conf"
	case "loadbalancer":
		return "/etc/haproxy/haproxy.cfg"
	case "logging":
		return "/etc/rsyslog.d/50-default.conf"
	case "mail":
		return "/etc/postfix/main.cf"
	default:
		return "/etc/app/config.yaml"
	}
}

func serviceFor(role string) string {
	switch role {
	case "web", "api":
		return "nginx"
	case "database":
		return "postgresql"
	case "cache":
		return "redis-server"
	case "loadbalancer":
		return "haproxy"
	case "monitoring":
		return "prometheus"
	case "logging":
		return "rsyslog"
	case "mail":
		return "postfix"
	case "ci":
		return "docker"
	default:
		return "app"
	}
}

func failingPackageFor(node demodata.Node) string {
	if node.Platform.Family == "windows" {
		return "Microsoft .NET 8 Runtime"
	}
	switch node.Role {
	case "web", "api":
		return "nginx-extras"
	case "loadbalancer":
		return "keepalived"
	case "ci":
		return "docker-buildx-plugin"
	default:
		return "app-agent"
	}
}
