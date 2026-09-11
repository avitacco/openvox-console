package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

type fakeReportFetcher struct {
	mu      sync.Mutex
	reports map[string][]openvoxdb.Report
	calls   int
}

func (f *fakeReportFetcher) Reports(_ context.Context, certname string) ([]openvoxdb.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.reports[certname], nil
}

func (f *fakeReportFetcher) setReports(certname string, reports []openvoxdb.Report) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reports == nil {
		f.reports = map[string][]openvoxdb.Report{}
	}
	f.reports[certname] = reports
}

func TestReportCorrelator_FindsAndLinksReport(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	since := job.StartedAt

	fetcher := &fakeReportFetcher{}
	fetcher.setReports("web01.example.com", []openvoxdb.Report{
		{Hash: "old-report", Certname: "web01.example.com", StartTime: since.Add(-time.Hour)},
		{Hash: "new-report", Certname: "web01.example.com", StartTime: since.Add(time.Second)},
	})

	c := newReportCorrelator(store, fetcher, nil, 10*time.Millisecond, 3)
	c.Correlate(ctx, job.ID, "web01.example.com", since)

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Targets[0].ReportHash != "new-report" {
		t.Errorf("ReportHash = %q, want new-report (the one at/after job start, not the older one)", got.Targets[0].ReportHash)
	}
}

func TestReportCorrelator_RetriesUntilReportAppears(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	since := job.StartedAt

	fetcher := &fakeReportFetcher{}
	// No reports yet - Correlate should retry rather than giving up
	// immediately.

	c := newReportCorrelator(store, fetcher, nil, 10*time.Millisecond, 5)

	done := make(chan struct{})
	go func() {
		c.Correlate(ctx, job.ID, "web01.example.com", since)
		close(done)
	}()

	// Let it retry a couple of times with nothing found, then make the
	// report appear.
	time.Sleep(35 * time.Millisecond)
	fetcher.setReports("web01.example.com", []openvoxdb.Report{
		{Hash: "delayed-report", Certname: "web01.example.com", StartTime: since.Add(time.Second)},
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Correlate() did not return after the report appeared")
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Targets[0].ReportHash != "delayed-report" {
		t.Errorf("ReportHash = %q, want delayed-report", got.Targets[0].ReportHash)
	}
}

func TestReportCorrelator_GivesUpAfterMaxAttempts(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	fetcher := &fakeReportFetcher{} // never has any reports

	c := newReportCorrelator(store, fetcher, nil, 5*time.Millisecond, 3)
	c.Correlate(ctx, job.ID, "web01.example.com", job.StartedAt)

	fetcher.mu.Lock()
	calls := fetcher.calls
	fetcher.mu.Unlock()
	if calls != 3 {
		t.Errorf("Reports() called %d times, want 3 (maxAttempts)", calls)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Targets[0].ReportHash != "" {
		t.Errorf("ReportHash = %q, want empty (never found)", got.Targets[0].ReportHash)
	}
}

func TestMostRecentReportSince(t *testing.T) {
	since := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	reports := []openvoxdb.Report{
		{Hash: "before", StartTime: since.Add(-time.Minute)},
		{Hash: "first-after", StartTime: since.Add(time.Minute)},
		{Hash: "latest", StartTime: since.Add(2 * time.Minute)},
	}
	if got := mostRecentReportSince(reports, since); got != "latest" {
		t.Errorf("mostRecentReportSince() = %q, want latest", got)
	}
	if got := mostRecentReportSince(nil, since); got != "" {
		t.Errorf("mostRecentReportSince(nil) = %q, want empty", got)
	}
	onlyOld := []openvoxdb.Report{{Hash: "too-old", StartTime: since.Add(-time.Hour)}}
	if got := mostRecentReportSince(onlyOld, since); got != "" {
		t.Errorf("mostRecentReportSince(onlyOld) = %q, want empty", got)
	}
}
