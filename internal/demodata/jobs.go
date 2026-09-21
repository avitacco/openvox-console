package demodata

import "time"

// Job is one orchestration run in the demo history.
//
// Unlike the fleet, this history is fabricated rather than a record of
// work that happened: no task was dispatched and no node ran anything.
// It is here so the Jobs page has something to show. It is fine in a
// screenshot and it is not evidence of anything - see marketing/README.md.
type Job struct {
	// Kind is "run", "task" or "plan".
	Kind string
	// TaskName is set for Kind "task", PlanName for Kind "plan".
	TaskName string
	PlanName string
	// Params is the task's parameters as a JSON object, or empty.
	Params string
	// TriggeredBy is the username or service token behind the run.
	TriggeredBy string
	// Age is how long before the demo Instant the job started.
	Age time.Duration
	// Targets are the certnames it ran against, with each one's outcome.
	Targets []JobTarget
}

// JobTarget is one node's outcome within a job.
type JobTarget struct {
	Certname string
	// Status is "succeeded" or "failed".
	Status   string
	ExitCode int
	Output   string
	Error    string
}

// Succeeded reports whether every target succeeded, which decides the
// job's own status.
func (j Job) Succeeded() bool {
	for _, t := range j.Targets {
		if t.Status != "succeeded" {
			return false
		}
	}
	return true
}

// Jobs is the demo orchestration history: a mix of ad-hoc runs, tasks
// and a plan, some clean and some with failures, spread over the days
// before the demo instant so the list is not all one timestamp.
var Jobs = buildJobs()

func buildJobs() []Job {
	const (
		ok   = "succeeded"
		fail = "failed"
	)

	okTarget := func(certname, output string) JobTarget {
		return JobTarget{Certname: certname, Status: ok, ExitCode: 0, Output: output}
	}
	failTarget := func(certname, detail string) JobTarget {
		return JobTarget{Certname: certname, Status: fail, ExitCode: 1, Error: detail}
	}

	return []Job{
		{
			Kind:        "run",
			TriggeredBy: "o.operator",
			Age:         18 * time.Minute,
			Targets: []JobTarget{
				okTarget("web-01.prod."+Domain, "Notice: Applied catalog in 11.42 seconds"),
				okTarget("web-02.prod."+Domain, "Notice: Applied catalog in 10.98 seconds"),
				okTarget("web-03.prod."+Domain, "Notice: Applied catalog in 12.31 seconds"),
			},
		},
		{
			Kind:        "task",
			TaskName:    "package::status",
			Params:      `{"name":"nginx"}`,
			TriggeredBy: "o.operator",
			Age:         52 * time.Minute,
			Targets: []JobTarget{
				okTarget("web-01.prod."+Domain, "nginx 1.22.1-9 (installed)"),
				okTarget("web-02.prod."+Domain, "nginx 1.22.1-9 (installed)"),
				okTarget("web-04.prod."+Domain, "nginx 1.22.1-9 (installed)"),
				failTarget("web-05.prod."+Domain, "connection closed before the task reported a result"),
			},
		},
		{
			Kind:        "task",
			TaskName:    "service::restart",
			Params:      `{"name":"haproxy"}`,
			TriggeredBy: "o.operator",
			Age:         3 * time.Hour,
			Targets: []JobTarget{
				okTarget("lb-01.prod."+Domain, "haproxy restarted"),
				failTarget("lb-02.prod."+Domain, "Job for haproxy.service failed: configuration check failed"),
			},
		},
		{
			Kind:        "plan",
			PlanName:    "deploy::rolling_restart",
			TriggeredBy: "r.engineer",
			Age:         6 * time.Hour,
			Targets: []JobTarget{
				okTarget("web-01.prod."+Domain, "drained, restarted, returned to pool"),
				okTarget("web-02.prod."+Domain, "drained, restarted, returned to pool"),
				okTarget("web-03.prod."+Domain, "drained, restarted, returned to pool"),
				okTarget("web-04.prod."+Domain, "drained, restarted, returned to pool"),
			},
		},
		{
			Kind:        "run",
			TriggeredBy: "ci-deploy-pipeline",
			Age:         26 * time.Hour,
			Targets: []JobTarget{
				okTarget("api-01.team-a."+Domain, "Notice: Applied catalog in 9.77 seconds"),
				okTarget("api-02.team-a."+Domain, "Notice: Applied catalog in 9.41 seconds"),
			},
		},
		{
			Kind:        "task",
			TaskName:    "facts::upload",
			TriggeredBy: "c.editor",
			Age:         30 * time.Hour,
			Targets: []JobTarget{
				okTarget("db-01.prod."+Domain, "facts uploaded"),
				okTarget("db-02.prod."+Domain, "facts uploaded"),
				okTarget("db-03.prod."+Domain, "facts uploaded"),
			},
		},
		{
			Kind:        "run",
			TriggeredBy: "o.operator",
			Age:         2 * 24 * time.Hour,
			Targets: []JobTarget{
				okTarget("app-win-01.prod."+Domain, "Notice: Applied catalog in 21.05 seconds"),
				okTarget("app-win-02.prod."+Domain, "Notice: Applied catalog in 22.63 seconds"),
				failTarget("app-win-03.prod."+Domain, "node did not respond within the dispatch timeout"),
			},
		},
		{
			Kind:        "task",
			TaskName:    "package::install",
			Params:      `{"name":"borgbackup","version":"1.2.8-1"}`,
			TriggeredBy: "o.operator",
			Age:         3 * 24 * time.Hour,
			Targets: []JobTarget{
				okTarget("backup-01.prod."+Domain, "borgbackup 1.2.8-1 installed"),
			},
		},
	}
}

// JobDuration is how long a demo job took, derived from its target count
// so that a job with more nodes plausibly takes longer - and derived
// rather than random, so it is identical on every seed.
func JobDuration(j Job) time.Duration {
	return time.Duration(len(j.Targets)) * 7 * time.Second
}
