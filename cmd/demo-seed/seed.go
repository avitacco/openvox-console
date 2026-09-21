package main

import (
	"context"
	"fmt"
)

// stage is one step of the seed. Stages run in order; each is expected to
// converge rather than accumulate when run a second time (see the
// repeatability requirement in specs/demo-data-seeding).
type stage struct {
	name string
	run  func(context.Context, options) error
}

// seed runs every stage in order, reporting progress as it goes. A stage
// that fails stops the run: a half-seeded console produces screenshots
// that are wrong in ways nobody notices, which is worse than no
// screenshots at all.
func seed(ctx context.Context, opts options) error {
	stages := []stage{}
	if opts.reset {
		stages = append(stages, stage{name: "reset (remove previously seeded nodes)", run: resetFleet})
	}
	stages = append(stages,
		stage{name: "fleet (facts, catalogs and reports into openvoxdb)", run: seedFleet},
		stage{name: "console records (groups, roles, users, service tokens)", run: seedConsole},
		stage{name: "code deploys (real g10k runs against the fixture repos)", run: seedDeploys},
		stage{name: "job history (fabricated, via the orchestrator's own store)", run: seedJobs},
		stage{name: "vulnerability findings (via the engine's own apply path)", run: seedVulnerabilities},
	)

	for i, s := range stages {
		fmt.Printf("[%d/%d] %s\n", i+1, len(stages), s.name)
		if err := s.run(ctx, opts); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}

	fmt.Printf("Seeded %s\n", opts.consoleURL)
	return nil
}
