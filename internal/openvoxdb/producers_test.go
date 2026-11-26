package openvoxdb

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestReportProducers(t *testing.T) {
	c, gotQuery := fakeQueryServer(t, `[{"producer": "compile1.example.com"}, {"producer": null}, {"producer": "compile2.example.com"}]`)
	since := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	got, err := c.ReportProducers(context.Background(), since)
	if err != nil {
		t.Fatalf("ReportProducers: %v", err)
	}
	if want := []string{"compile1.example.com", "compile2.example.com"}; !slices.Equal(got, want) {
		t.Errorf("producers = %v, want %v", got, want)
	}
	if want := `reports[producer] { receive_time > "2026-11-19T12:00:00Z" group by producer }`; *gotQuery != want {
		t.Errorf("query = %s, want %s", *gotQuery, want)
	}
}
