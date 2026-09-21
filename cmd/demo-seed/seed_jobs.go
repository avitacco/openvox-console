package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
)

// seedJobs writes the demo orchestration history through
// orchestrator.Store - the same type the orchestrator itself writes job
// records with.
//
// It goes through the store rather than the console's HTTP API because
// there is no API for recording a job that already happened: POST
// /orchestrator/runs dispatches work to live nodes, and the demo fleet
// has none. Statements written against the tables by hand were the other
// option and are worse: they keep succeeding after a schema change while
// meaning something different, whereas this stops compiling.
//
// The store is given the demo clock, so this history sits at the same
// fixed instant as everything else rather than at wall-clock now. Without
// that the Jobs page would date every job to whenever the seed last ran,
// and its screenshot would change on every refresh.
func seedJobs(ctx context.Context, opts options) error {
	if opts.postgresDSN == "" {
		return fmt.Errorf("no console Postgres DSN: pass --postgres-dsn or set CONSOLE_POSTGRES_DSN (job history has no HTTP write contract, so it is written through the console's own store)")
	}

	db, err := persistence.Connect(ctx, opts.postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	existing, err := existingDemoJobCount(ctx, db)
	if err != nil {
		return err
	}
	if existing >= len(demodata.Jobs) {
		fmt.Printf("    %d demo job(s) already present, leaving them alone\n", existing)
		return nil
	}

	for _, job := range demodata.Jobs {
		if err := writeJob(ctx, db, job); err != nil {
			return fmt.Errorf("writing job %s: %w", describeJob(job), err)
		}
	}

	fmt.Printf("    %d jobs with %d target results\n", len(demodata.Jobs), countTargets())
	return nil
}

// writeJob creates one job and records each target's outcome, with the
// store's clock moved to that job's place in the demo timeline so its
// started_at and finished_at land where they belong.
func writeJob(ctx context.Context, db *persistence.DB, job demodata.Job) error {
	startedAt := demodata.Ago(job.Age)
	finishedAt := startedAt.Add(demodata.JobDuration(job))

	// The store reads its clock once per write, so stepping it between
	// calls is what places start and finish at different times.
	at := startedAt
	store := orchestrator.NewStoreWithClock(db.Pool, func() time.Time { return at })

	targets := make([]string, 0, len(job.Targets))
	for _, t := range job.Targets {
		targets = append(targets, t.Certname)
	}

	var params json.RawMessage
	if job.Params != "" {
		params = json.RawMessage(job.Params)
	}

	created, err := store.CreateJob(ctx, orchestrator.JobKind(job.Kind), job.TaskName, job.PlanName, params, targets, job.TriggeredBy)
	if err != nil {
		return err
	}

	at = finishedAt
	for _, t := range job.Targets {
		exitCode := t.ExitCode
		if err := store.RecordTargetResult(ctx, created.ID, t.Certname, t.Status, &exitCode, t.Output, t.Error); err != nil {
			return fmt.Errorf("target %s: %w", t.Certname, err)
		}
	}

	status := orchestrator.StatusSucceeded
	if !job.Succeeded() {
		status = orchestrator.StatusFailed
	}
	return store.CompleteJob(ctx, created.ID, status)
}

// existingDemoJobCount reports how many of the demo jobs the console
// already holds.
//
// Job history is append-only - there is no natural key to update in
// place - so converging means not writing it a second time.
//
// It counts by triggering actor rather than counting every job in the
// console. A development database accumulates job rows from test runs
// and earlier experiments; a plain total says "there are already jobs
// here" and the demo history never gets written. The demo usernames are
// created by this same seed and appear on nothing else, which makes them
// an exact discriminator.
func existingDemoJobCount(ctx context.Context, db *persistence.DB) (int, error) {
	store := orchestrator.NewStore(db.Pool)

	var total int
	for _, actor := range demoJobActors() {
		_, n, err := store.ListJobs(ctx, 1, 1, orchestrator.JobFilter{TriggeredBy: actor})
		if err != nil {
			return 0, fmt.Errorf("counting existing jobs for %q: %w", actor, err)
		}
		total += n
	}
	return total, nil
}

// demoJobActors are the distinct actors the demo history is attributed
// to, in a stable order.
func demoJobActors() []string {
	seen := map[string]bool{}
	var actors []string
	for _, j := range demodata.Jobs {
		if !seen[j.TriggeredBy] {
			seen[j.TriggeredBy] = true
			actors = append(actors, j.TriggeredBy)
		}
	}
	return actors
}

func countTargets() int {
	var n int
	for _, j := range demodata.Jobs {
		n += len(j.Targets)
	}
	return n
}

func describeJob(job demodata.Job) string {
	switch {
	case job.TaskName != "":
		return "task " + job.TaskName
	case job.PlanName != "":
		return "plan " + job.PlanName
	default:
		return "run"
	}
}
