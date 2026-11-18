// Package plans keeps saved plans: named, ordered lists of Puppet runs
// and tasks that can be run again and again (see openspec/specs/plans).
//
// A saved plan is a template and nothing more. It names no nodes and
// holds no sensitive value; running one is an ordinary plan request to
// the orchestrator carrying the plan's steps, the values it lacks and
// the nodes to run on. There is deliberately no "run by id": whoever
// starts a plan sends, and so has seen, exactly what will run.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// MaxDescriptionLength bounds a plan's description.
const MaxDescriptionLength = 500

// ErrNotFound is a saved plan that does not exist.
var ErrNotFound = errors.New("plan not found")

// InvalidPlanError is a plan refused for one of its own fields.
type InvalidPlanError struct {
	Field  string
	Reason string
}

func (e *InvalidPlanError) Error() string { return e.Reason }

// Step is a saved plan's step as it is read: the stored step, and what
// running it will need.
type Step struct {
	orchestrator.PlanStep
	// Needs are the sensitive parameters of a task step's task. Their
	// values are not kept and are asked for when the plan is run.
	Needs []orchestrator.SensitiveNeed `json:"needs,omitempty"`
	// Problem says why the step can no longer run as saved: its task
	// or environment has gone.
	Problem string `json:"problem,omitempty"`
	// NotPermitted is set, with Problem, on a task step whose task the
	// caller reading the plan may not run. The step is still shown: it
	// is the caller's roles that cannot run it, not the plan that is
	// broken.
	NotPermitted bool `json:"notPermitted,omitempty"`
}

// Plan is a saved plan.
type Plan struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Steps       []Step    `json:"steps,omitempty"`
	StepCount   int       `json:"stepCount"`
	CreatedBy   string    `json:"createdBy"`
	UpdatedBy   string    `json:"updatedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Input is a saved plan as a caller writes it.
type Input struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Steps       []orchestrator.PlanStep `json:"steps"`
}

// Store persists saved plans.
type Store struct{ pool *pgxpool.Pool }

// NewStore builds a Store over pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const planColumns = `id, name, description, steps, created_by, updated_by, created_at, updated_at`

func scanPlan(row pgx.Row) (Plan, []orchestrator.PlanStep, error) {
	var p Plan
	var raw []byte
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &raw, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Plan{}, nil, err
	}
	var steps []orchestrator.PlanStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return Plan{}, nil, fmt.Errorf("decode plan %d steps: %w", p.ID, err)
	}
	p.StepCount = len(steps)
	return p, steps, nil
}

// nameTaken turns a unique violation on the name into a refusal.
func nameTaken(err error, name string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return &InvalidPlanError{Field: "name", Reason: fmt.Sprintf("a plan named %q already exists", name)}
	}
	return err
}

// Create stores a new plan.
func (s *Store) Create(ctx context.Context, name, description string, steps []orchestrator.PlanStep, by string) (Plan, []orchestrator.PlanStep, error) {
	raw, err := json.Marshal(steps)
	if err != nil {
		return Plan{}, nil, fmt.Errorf("encode steps: %w", err)
	}
	p, kept, err := scanPlan(s.pool.QueryRow(ctx,
		`INSERT INTO plans (name, description, steps, created_by, updated_by) VALUES ($1, $2, $3, $4, $4) RETURNING `+planColumns,
		name, description, raw, by))
	if err != nil {
		return Plan{}, nil, nameTaken(err, name)
	}
	return p, kept, nil
}

// Replace stores new contents for plan id.
func (s *Store) Replace(ctx context.Context, id int64, name, description string, steps []orchestrator.PlanStep, by string) (Plan, []orchestrator.PlanStep, error) {
	raw, err := json.Marshal(steps)
	if err != nil {
		return Plan{}, nil, fmt.Errorf("encode steps: %w", err)
	}
	p, kept, err := scanPlan(s.pool.QueryRow(ctx,
		`UPDATE plans SET name = $2, description = $3, steps = $4, updated_by = $5, updated_at = now() WHERE id = $1 RETURNING `+planColumns,
		id, name, description, raw, by))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, nil, ErrNotFound
	}
	if err != nil {
		return Plan{}, nil, nameTaken(err, name)
	}
	return p, kept, nil
}

// Get returns plan id and its stored steps.
func (s *Store) Get(ctx context.Context, id int64) (Plan, []orchestrator.PlanStep, error) {
	p, steps, err := scanPlan(s.pool.QueryRow(ctx, `SELECT `+planColumns+` FROM plans WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, nil, ErrNotFound
	}
	return p, steps, err
}

// List returns every plan by name, without its steps.
func (s *Store) List(ctx context.Context) ([]Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planColumns+` FROM plans ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	out := make([]Plan, 0)
	for rows.Next() {
		p, _, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("list plans: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete removes plan id and returns it as it was.
func (s *Store) Delete(ctx context.Context, id int64) (Plan, error) {
	p, _, err := scanPlan(s.pool.QueryRow(ctx, `DELETE FROM plans WHERE id = $1 RETURNING `+planColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	return p, err
}

// Checker is the part of *orchestrator.Starter saved plans use.
type Checker interface {
	CheckPlanTemplate(ctx context.Context, name string, steps []orchestrator.PlanStep, tasks taskscope.Scope) ([]orchestrator.PlanStep, error)
	StepNeeds(ctx context.Context, step orchestrator.PlanStep) ([]orchestrator.SensitiveNeed, string)
}

// Service is saved plans' rules.
type Service struct {
	store   *Store
	checker Checker

	recordActivity func(action, actor, summary string)
	recordAudit    func(auditlog.Event)
}

// NewService builds a Service.
func NewService(store *Store, checker Checker) *Service {
	return &Service{
		store: store, checker: checker,
		recordActivity: func(string, string, string) {},
		recordAudit:    func(auditlog.Event) {},
	}
}

// SetRecorders attaches the activity and audit logs.
func (s *Service) SetRecorders(activity func(action, actor, summary string), audit func(auditlog.Event)) {
	if activity != nil {
		s.recordActivity = activity
	}
	if audit != nil {
		s.recordAudit = audit
	}
}

func (s *Service) record(action, actor string, p Plan, summary string) {
	s.recordActivity(action, actor, summary)
	s.recordAudit(auditlog.Event{Action: action, Actor: actor, ResourceType: "plan", ResourceID: strconv.FormatInt(p.ID, 10)})
}

// check validates in and returns the steps as they may be stored.
//
// tasks is what the caller saving the plan may run: a task step whose task
// they may not run is refused, naming the step.
func (s *Service) check(ctx context.Context, in *Input, tasks taskscope.Scope) ([]orchestrator.PlanStep, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return nil, &InvalidPlanError{Field: "name", Reason: "a plan needs a name"}
	}
	if len([]rune(in.Name)) > orchestrator.MaxPlanNameLength {
		return nil, &InvalidPlanError{Field: "name", Reason: fmt.Sprintf("a plan's name may be at most %d characters", orchestrator.MaxPlanNameLength)}
	}
	if len([]rune(in.Description)) > MaxDescriptionLength {
		return nil, &InvalidPlanError{Field: "description", Reason: fmt.Sprintf("a plan's description may be at most %d characters", MaxDescriptionLength)}
	}
	return s.checker.CheckPlanTemplate(ctx, in.Name, in.Steps, tasks)
}

// withSteps fills p.Steps from the stored steps, with what each needs.
// A task step the reading caller may not run is marked so, decided from
// its name before the catalogue is read for it.
func (s *Service) withSteps(ctx context.Context, p Plan, stored []orchestrator.PlanStep, tasks taskscope.Scope) Plan {
	p.Steps = make([]Step, len(stored))
	for i, step := range stored {
		if step.Kind == orchestrator.JobKindTask && !tasks.Allows(step.TaskName) {
			p.Steps[i] = Step{PlanStep: step, NotPermitted: true,
				Problem: fmt.Sprintf("you are not allowed to run the task %q", step.TaskName)}
			continue
		}
		needs, problem := s.checker.StepNeeds(ctx, step)
		p.Steps[i] = Step{PlanStep: step, Needs: needs, Problem: problem}
	}
	return p
}

// List returns every saved plan, without steps.
func (s *Service) List(ctx context.Context) ([]Plan, error) { return s.store.List(ctx) }

// Get returns one saved plan with its steps.
func (s *Service) Get(ctx context.Context, id int64, tasks taskscope.Scope) (Plan, error) {
	p, stored, err := s.store.Get(ctx, id)
	if err != nil {
		return Plan{}, err
	}
	return s.withSteps(ctx, p, stored, tasks), nil
}

// Create saves a new plan.
func (s *Service) Create(ctx context.Context, in Input, actor string, tasks taskscope.Scope) (Plan, error) {
	steps, err := s.check(ctx, &in, tasks)
	if err != nil {
		return Plan{}, err
	}
	p, stored, err := s.store.Create(ctx, in.Name, in.Description, steps, actor)
	if err != nil {
		return Plan{}, err
	}
	s.record("plan.created", actor, p, fmt.Sprintf("created plan %q (%d steps)", p.Name, p.StepCount))
	return s.withSteps(ctx, p, stored, tasks), nil
}

// Replace changes a saved plan.
func (s *Service) Replace(ctx context.Context, id int64, in Input, actor string, tasks taskscope.Scope) (Plan, error) {
	if _, _, err := s.store.Get(ctx, id); err != nil {
		return Plan{}, err
	}
	steps, err := s.check(ctx, &in, tasks)
	if err != nil {
		return Plan{}, err
	}
	p, stored, err := s.store.Replace(ctx, id, in.Name, in.Description, steps, actor)
	if err != nil {
		return Plan{}, err
	}
	s.record("plan.updated", actor, p, fmt.Sprintf("changed plan %q (%d steps)", p.Name, p.StepCount))
	return s.withSteps(ctx, p, stored, tasks), nil
}

// Delete removes a saved plan. Jobs it started are not touched.
func (s *Service) Delete(ctx context.Context, id int64, actor string) error {
	p, err := s.store.Delete(ctx, id)
	if err != nil {
		return err
	}
	s.record("plan.deleted", actor, p, fmt.Sprintf("deleted plan %q", p.Name))
	return nil
}
