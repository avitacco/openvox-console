package codeplans

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Catalog lists and reads plan files. *Loader is one.
type Catalog interface {
	Source
	Files(env string) ([]File, error)
}

// Handlers serves the code plans API.
type Handlers struct {
	catalog     Catalog
	coordinator *Coordinator
	actor       func(r *http.Request) string
	scope       nodescope.Func
	taskScope   taskscope.Func
}

// NewHandlers builds Handlers. scope is the caller's node scope for
// orchestrator:run and taskScope the tasks they may run; both are asked
// when a plan is started and held for its whole run.
func NewHandlers(catalog Catalog, coordinator *Coordinator, actor func(r *http.Request) string) *Handlers {
	return &Handlers{catalog: catalog, coordinator: coordinator, actor: actor, scope: nodescope.Unscoped, taskScope: taskscope.UnrestrictedFunc}
}

// SetScope limits each caller to the nodes in their node group scope.
func (h *Handlers) SetScope(s nodescope.Func) { h.scope = s }

// SetTaskScope limits each caller to the tasks their roles allow.
func (h *Handlers) SetTaskScope(s taskscope.Func) { h.taskScope = s }

// Register wires the routes. See internal/rbac.Verifier.Authorize.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/code-plans", authorize("orchestrator:read", h.list))
	mux.HandleFunc("GET /api/v1/code-plans/{name}", authorize("orchestrator:read", h.get))
	mux.HandleFunc("POST /api/v1/orchestrator/code-plans", authorize("orchestrator:run", h.start))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// ParamInfo describes a parameter for a form.
type ParamInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Sensitive   bool   `json:"sensitive,omitempty"`
	// Target says the parameter takes nodes: names, a group or a query.
	Target  bool `json:"target,omitempty"`
	Default any  `json:"default,omitempty"`
}

// StepInfo describes a step for display.
type StepInfo struct {
	Index       int    `json:"index"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	Task        string `json:"task,omitempty"`
	Plan        string `json:"plan,omitempty"`
}

// Info is a plan as listed.
type Info struct {
	Name        string      `json:"name"`
	Module      string      `json:"module"`
	Path        string      `json:"path"`
	Description string      `json:"description,omitempty"`
	Usable      bool        `json:"usable"`
	Reason      string      `json:"reason,omitempty"`
	Parameters  []ParamInfo `json:"parameters,omitempty"`
	Steps       []StepInfo  `json:"steps,omitempty"`
}

func describe(f File) Info {
	info := Info{Name: f.Name, Module: f.Module, Path: f.Path}
	plan, err := Parse(f)
	if err != nil {
		info.Reason = err.Error()
		return info
	}
	info.Usable, info.Description = true, plan.Description
	for _, p := range plan.Parameters {
		pi := ParamInfo{Name: p.Name, Type: p.TypeText, Description: p.Description, Required: p.Required, Sensitive: p.Sensitive, Target: p.Target}
		if p.HasDefault {
			if v, err := Eval(p.Default, Env{}); err == nil {
				pi.Default = v
			}
		}
		info.Parameters = append(info.Parameters, pi)
	}
	for _, s := range plan.Steps {
		info.Steps = append(info.Steps, StepInfo{Index: s.Index, Name: s.Name, Description: s.Description, Kind: s.Kind, Task: s.Task, Plan: s.Plan})
	}
	return info
}

func environmentOf(r *http.Request) string {
	if e := r.URL.Query().Get("environment"); e != "" {
		return e
	}
	return orchestrator.DefaultTaskEnvironment
}

// catalogError answers a failure to read plans. No code on this instance
// is said so, which is not the same as no plans.
func catalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotAvailable):
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "message": err.Error(), "items": []Info{}})
	case errors.Is(err, ErrEnvironmentNotFound):
		writeError(w, http.StatusNotFound, "environment not found")
	case errors.Is(err, ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "plan not found")
	default:
		writeError(w, http.StatusInternalServerError, "plans could not be read")
	}
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	env := environmentOf(r)
	files, err := h.catalog.Files(env)
	if err != nil && len(files) == 0 {
		catalogError(w, err)
		return
	}
	items := make([]Info, 0, len(files))
	for _, f := range files {
		info := describe(f)
		info.Steps = nil // the list is for choosing; a plan's steps are its own page
		items = append(items, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "environment": env, "items": items})
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	f, err := h.catalog.File(environmentOf(r), r.PathValue("name"))
	if err != nil {
		catalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, describe(f))
}

type startRequest struct {
	Plan        string         `json:"plan"`
	Environment string         `json:"environment"`
	Parameters  map[string]any `json:"parameters"`
}

func (h *Handlers) start(w http.ResponseWriter, r *http.Request) {
	// Numbers stay numbers, so an Integer is told from a Float.
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var body startRequest
	if err := dec.Decode(&body); err != nil || body.Plan == "" {
		writeError(w, http.StatusBadRequest, "give the plan to run")
		return
	}
	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not determine which nodes you may act on: "+err.Error())
		return
	}
	tasks, err := h.taskScope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not determine which tasks you may run: "+err.Error())
		return
	}
	env := body.Environment
	if env == "" {
		env = orchestrator.DefaultTaskEnvironment
	}
	job, err := h.coordinator.Start(r.Context(), Request{
		Plan: body.Plan, Environment: env, Parameters: body.Parameters,
		Start: orchestrator.StartRequest{Scope: scope, Tasks: tasks, Actor: h.actor(r)},
	})
	if err != nil {
		var run *RunError
		var param *ParamError
		switch {
		case errors.Is(err, ErrNotAvailable):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, ErrEnvironmentNotFound):
			writeError(w, http.StatusBadRequest, "environment "+env+" was not found")
		case errors.As(err, &param):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "field": param.Name})
		case errors.As(err, &run):
			writeError(w, http.StatusBadRequest, run.Reason)
		default:
			status, out := orchestrator.RefusalResponse(err)
			writeJSON(w, status, out)
		}
		return
	}
	writeJSON(w, http.StatusOK, job)
}
