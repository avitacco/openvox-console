// Command gen renders the console's static HTML pages from
// frontend/templates/layout.html.tmpl (the shared head/header
// boilerplate) plus one frontend/templates/pages/*.tmpl per page
// (the page-specific <main> content). Invoked by frontend/build.sh,
// not part of the served binary.
package main

import (
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type page struct {
	Name       string // output filename, e.g. "index.html"
	Title      string
	Script     string // page JS entry, without the .js extension
	ShowHeader bool
	ActiveNav  string // "nodes" | "node-connectivity" | "packages" | "vulnerabilities" | "groups" | "admin" | "activity" | "system" | "code" | "jobs" | "" (preferences.html deliberately has none - it's reached from the header's user menu, not the sidenav)
}

var pages = []page{
	{Name: "index.html", Title: "OpenVox Console", Script: "index", ShowHeader: true, ActiveNav: "nodes"},
	{Name: "node.html", Title: "Node - OpenVox Console", Script: "node", ShowHeader: true, ActiveNav: "nodes"},
	{Name: "report.html", Title: "Report - OpenVox Console", Script: "report", ShowHeader: true, ActiveNav: "nodes"},
	{Name: "nodes.html", Title: "Nodes - OpenVox Console", Script: "nodes", ShowHeader: true, ActiveNav: "node-connectivity"},
	{Name: "packages.html", Title: "Packages - OpenVox Console", Script: "packages", ShowHeader: true, ActiveNav: "packages"},
	{Name: "vulnerabilities.html", Title: "Vulnerabilities - OpenVox Console", Script: "vulnerabilities", ShowHeader: true, ActiveNav: "vulnerabilities"},
	{Name: "vulnerability.html", Title: "Vulnerability - OpenVox Console", Script: "vulnerability", ShowHeader: true, ActiveNav: "vulnerabilities"},
	{Name: "vulnerability-providers.html", Title: "Vulnerability Providers - OpenVox Console", Script: "vulnerability-providers", ShowHeader: true, ActiveNav: "vulnerabilities"},
	{Name: "group.html", Title: "Node Group - OpenVox Console", Script: "group", ShowHeader: true, ActiveNav: "groups"},
	{Name: "groups.html", Title: "Node Groups - OpenVox Console", Script: "groups", ShowHeader: true, ActiveNav: "groups"},
	{Name: "users.html", Title: "Users - OpenVox Console", Script: "users", ShowHeader: true, ActiveNav: "admin"},
	{Name: "roles.html", Title: "Roles - OpenVox Console", Script: "roles", ShowHeader: true, ActiveNav: "admin"},
	{Name: "service-tokens.html", Title: "Service Tokens - OpenVox Console", Script: "service-tokens", ShowHeader: true, ActiveNav: "admin"},
	{Name: "activity.html", Title: "Activity - OpenVox Console", Script: "activity", ShowHeader: true, ActiveNav: "activity"},
	{Name: "status.html", Title: "Stack Status - OpenVox Console", Script: "status", ShowHeader: true, ActiveNav: "system"},
	{Name: "code.html", Title: "Code - OpenVox Console", Script: "code", ShowHeader: true, ActiveNav: "code"},
	// Redirects into code.html's Deploy history tab. Kept rather than
	// removed so existing bookmarks and the setup runbooks' links still
	// resolve - see deploys-redirect.js.
	{Name: "deploys.html", Title: "Deploys - OpenVox Console", Script: "deploys-redirect", ShowHeader: true, ActiveNav: "code"},
	{Name: "jobs.html", Title: "Jobs - OpenVox Console", Script: "jobs", ShowHeader: true, ActiveNav: "jobs"},
	{Name: "job.html", Title: "Job - OpenVox Console", Script: "job", ShowHeader: true, ActiveNav: "jobs"},
	{Name: "preferences.html", Title: "Preferences - OpenVox Console", Script: "preferences", ShowHeader: true},
	{Name: "login.html", Title: "Log in - OpenVox Console", Script: "login", ShowHeader: false},
	{Name: "oidc-callback.html", Title: "Signing in - OpenVox Console", Script: "oidc-callback", ShowHeader: false},
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen <output-dir>")
		os.Exit(1)
	}
	outDir := os.Args[1]

	layoutPath := filepath.Join("templates", "layout.html.tmpl")

	for _, p := range pages {
		pageTmplName := strings.TrimSuffix(p.Name, ".html") + ".tmpl"
		pagePath := filepath.Join("templates", "pages", pageTmplName)

		tmpl, err := template.ParseFiles(layoutPath, pagePath)
		if err != nil {
			log.Fatalf("parsing templates for %s: %v", p.Name, err)
		}

		outPath := filepath.Join(outDir, p.Name)
		f, err := os.Create(outPath)
		if err != nil {
			log.Fatalf("creating %s: %v", outPath, err)
		}
		if err := tmpl.ExecuteTemplate(f, "layout.html.tmpl", p); err != nil {
			f.Close()
			log.Fatalf("rendering %s: %v", p.Name, err)
		}
		if err := f.Close(); err != nil {
			log.Fatalf("closing %s: %v", outPath, err)
		}
	}

	fmt.Printf("Generated %d pages into %s\n", len(pages), outDir)
}
