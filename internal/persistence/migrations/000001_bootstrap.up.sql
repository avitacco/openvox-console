-- Phase 0 bootstrap migration. No feature tables yet - those land with the
-- phases that own them (classifier, RBAC, activity, code manager,
-- orchestrator). This establishes that migration tooling is wired up.
SELECT 1;
