package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// reportFetcher is the subset of openvoxdb.Client this package needs.
type reportFetcher interface {
	Reports(ctx context.Context, certname string) ([]openvoxdb.Report, error)
}

const (
	defaultCorrelateRetryInterval = 5 * time.Second
	defaultCorrelateMaxAttempts   = 6
)

// ReportCorrelator links a completed run's job target to the openvoxdb
// report it produced - best-effort, since openvoxdb ingestion happens
// asynchronously after a run completes and the two have no shared
// identifier (see design.md's "Report correlation" decision).
type ReportCorrelator struct {
	store         *Store
	client        reportFetcher
	logger        *slog.Logger
	retryInterval time.Duration
	maxAttempts   int
}

// NewReportCorrelator builds a ReportCorrelator with production retry
// settings. logger may be nil (slog.Default() is used).
func NewReportCorrelator(store *Store, client reportFetcher, logger *slog.Logger) *ReportCorrelator {
	return newReportCorrelator(store, client, logger, defaultCorrelateRetryInterval, defaultCorrelateMaxAttempts)
}

func newReportCorrelator(store *Store, client reportFetcher, logger *slog.Logger, retryInterval time.Duration, maxAttempts int) *ReportCorrelator {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReportCorrelator{store: store, client: client, logger: logger, retryInterval: retryInterval, maxAttempts: maxAttempts}
}

// Correlate attempts to find and link the report openvoxdb ingested
// for a completed run against certname, started at or after since.
// Retries on an interval (ingestion is asynchronous) up to a bounded
// number of attempts, then gives up - the job detail view still shows
// the job's own outcome independent of this link (see design.md's
// Risks). Intended to be run in its own goroutine; blocks until it
// either finds a report, gives up, or ctx is cancelled.
func (c *ReportCorrelator) Correlate(ctx context.Context, jobID int64, certname string, since time.Time) {
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		reports, err := c.client.Reports(ctx, certname)
		if err != nil {
			c.logger.Warn("failed to query openvoxdb for report correlation", "error", err, "certname", certname)
		} else if hash := mostRecentReportSince(reports, since); hash != "" {
			if err := c.store.SetTargetReport(ctx, jobID, certname, hash); err != nil {
				c.logger.Error("failed to record correlated report", "error", err, "jobID", jobID, "certname", certname)
			}
			return
		}

		select {
		case <-time.After(c.retryInterval):
		case <-ctx.Done():
			return
		}
	}
	c.logger.Warn("gave up correlating a report for a completed run", "jobID", jobID, "certname", certname)
}

// mostRecentReportSince returns the hash of the most recent report in
// reports whose StartTime is at or after since, or "" if none qualify.
func mostRecentReportSince(reports []openvoxdb.Report, since time.Time) string {
	var best openvoxdb.Report
	var found bool
	for _, r := range reports {
		if r.StartTime.Before(since) {
			continue
		}
		if !found || r.StartTime.After(best.StartTime) {
			best = r
			found = true
		}
	}
	if !found {
		return ""
	}
	return best.Hash
}
