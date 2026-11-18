package agentless

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type fakeNames struct {
	names []string
	err   error
	reads int
}

func (f *fakeNames) Names(context.Context) ([]string, error) {
	f.reads++
	return f.names, f.err
}

func TestDirectory_CachesAndInvalidates(t *testing.T) {
	src := &fakeNames{names: []string{"a", "b"}}
	d := NewDirectory(src, nil)
	now := time.Now()
	d.now = func() time.Time { return now }
	ctx := context.Background()

	if !d.Has(ctx, "a") || d.Has(ctx, "z") {
		t.Fatal("wrong membership")
	}
	if got := d.Among(ctx, []string{"z", "b", "a", "a"}); !slices.Equal(got, []string{"a", "a", "b"}) {
		t.Errorf("Among = %v", got)
	}
	if src.reads != 1 {
		t.Errorf("read %d times, want once within the TTL", src.reads)
	}

	src.names = []string{"c"}
	if d.Has(ctx, "c") {
		t.Error("a change was seen before the TTL or an invalidation")
	}
	d.Invalidate()
	if !d.Has(ctx, "c") || d.Has(ctx, "a") {
		t.Error("an invalidation did not reread the names")
	}
	now = now.Add(DirectoryTTL + time.Second)
	src.names = []string{"d"}
	if !d.Has(ctx, "d") {
		t.Error("the names were not reread after the TTL")
	}
}

func TestDirectory_AFailedReadKeepsWhatWasKnown(t *testing.T) {
	src := &fakeNames{names: []string{"a"}}
	d := NewDirectory(src, nil)
	now := time.Now()
	d.now = func() time.Time { return now }
	ctx := context.Background()
	d.Has(ctx, "a")
	now = now.Add(time.Minute)
	src.err = errors.New("database down")
	if !d.Has(ctx, "a") {
		t.Error("a target stopped being agentless because the database was unreachable")
	}
}
