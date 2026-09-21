package codemanager

// EnvironmentOwners maps a Puppet environment name to the source that
// deployed it. Built from deploy history rather than from prefix
// matching - see attributeEnvironments.
type EnvironmentOwners map[string]string

// attributeEnvironments builds the environment -> source map from what
// each source has actually deployed.
//
// Prefix matching is the obvious alternative and is wrong. At most one
// source may be unprefixed, and under prefix matching that source would
// claim every environment name no other prefix matches - including
// directories placed by hand, and environments left behind by a source
// since removed from the configuration. It would silently inflate
// exactly one repository's node counts with nodes that have nothing to
// do with it. Attributing only what a source is recorded as having
// deployed replaces that guess with a fact, at the cost of attributing
// nothing for environments deployed before this was tracked.
func attributeEnvironments(summaries map[string]SourceDeploySummary) EnvironmentOwners {
	owners := make(EnvironmentOwners)
	for source, summary := range summaries {
		for _, environment := range summary.Environments {
			owners[environment] = source
		}
	}
	return owners
}

// EnvironmentCounts is a tally of nodes per environment, plus the nodes
// that had no environment at all to be counted against.
type EnvironmentCounts struct {
	ByEnvironment map[string]int
	// Unknown counts nodes carrying no environment - a node that has
	// never reported, or one no group assigns an environment to.
	// Separate from any source's count because it is not evidence
	// about any repository.
	Unknown int
}

// tally buckets one node's environment.
func (c *EnvironmentCounts) tally(environment *string) {
	if environment == nil || *environment == "" {
		c.Unknown++
		return
	}
	if c.ByEnvironment == nil {
		c.ByEnvironment = make(map[string]int)
	}
	c.ByEnvironment[*environment]++
}

// forSource totals the counts for every environment owned by source.
func (c EnvironmentCounts) forSource(source string, owners EnvironmentOwners) int {
	var total int
	for environment, count := range c.ByEnvironment {
		if owners[environment] == source {
			total += count
		}
	}
	return total
}

// unattributed totals the counts for environments no source is
// recorded as having deployed. Reported separately rather than folded
// into any repository: these nodes are running or assigned to code the
// console cannot trace back to a configured repository, which is
// information in its own right, and attributing them to the unprefixed
// source would be a fabrication.
func (c EnvironmentCounts) unattributed(owners EnvironmentOwners) int {
	var total int
	for environment, count := range c.ByEnvironment {
		if _, owned := owners[environment]; !owned {
			total += count
		}
	}
	return total
}
