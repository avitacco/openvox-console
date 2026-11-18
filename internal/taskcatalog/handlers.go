package taskcatalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Handlers serves the task catalogue API. Every route requires
// orchestrator:read: the catalogue is what an operator browses before
// running a task, and reveals no more than the job pages already do.
type Handlers struct {
	client *Client

	// taskScope limits what the catalogue offers to the tasks the caller
	// may run - see SetTaskScope.
	taskScope taskscope.Func
}

// NewHandlers builds Handlers reading from client. A nil client is valid
// and answers every request with 503 "not configured".
func NewHandlers(client *Client) *Handlers {
	return &Handlers{client: client, taskScope: taskscope.UnrestrictedFunc}
}

// SetTaskScope limits the catalogue to the tasks each caller may run: the
// list omits the others and a request for one is answered as for a task
// that does not exist. Without it every caller sees every task.
func (h *Handlers) SetTaskScope(tasks taskscope.Func) { h.taskScope = tasks }

// Register wires the routes onto mux - see internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/tasks/environments", authorize("orchestrator:read", h.listEnvironments))
	mux.HandleFunc("GET /api/v1/tasks", authorize("orchestrator:read", h.listTasks))
	mux.HandleFunc("GET /api/v1/tasks/{module}/{task}", authorize("orchestrator:read", h.getTask))
}

// HTTPStatus maps a catalogue error to the status an API should answer
// with, so the orchestrator reports a task it cannot resolve the same way
// the catalogue itself does.
func HTTPStatus(err error) int {
	var nf *NotFoundError
	var ue *UnreachableError
	var ve *ValidationError
	switch {
	case errors.Is(err, ErrNotConfigured):
		return http.StatusServiceUnavailable
	case errors.As(err, &nf):
		return http.StatusNotFound
	case errors.As(err, &ve):
		return http.StatusBadRequest
	case errors.As(err, &ue):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// environmentParam is the request's environment, defaulting to
// production as Puppet does.
func environmentParam(r *http.Request) string {
	if env := r.URL.Query().Get("environment"); env != "" {
		return env
	}
	return "production"
}

func (h *Handlers) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := h.client.Environments(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

func (h *Handlers) listTasks(w http.ResponseWriter, r *http.Request) {
	env := environmentParam(r)
	scope, err := h.taskScope(r)
	if err != nil {
		writeError(w, err)
		return
	}
	tasks, err := h.client.Tasks(r.Context(), env)
	if err != nil {
		writeError(w, err)
		return
	}
	if scope.Restricted() {
		offered := make([]TaskSummary, 0, len(tasks))
		for _, t := range tasks {
			if scope.AllowsAnywhere(t.Name) {
				offered = append(offered, t)
			}
		}
		tasks = offered
	}
	writeJSON(w, http.StatusOK, map[string]any{"environment": env, "tasks": tasks})
}

// TaskDetail is one task as the API returns it.
type TaskDetail struct {
	Name         string `json:"name"`
	Environment  string `json:"environment"`
	Description  string `json:"description,omitempty"`
	SupportsNoop bool   `json:"supportsNoop"`
	Private      bool   `json:"private"`
	// AcceptsAnyParameters is true for a task whose metadata declares
	// no parameters at all, which Puppet lets take anything.
	AcceptsAnyParameters bool          `json:"acceptsAnyParameters"`
	Parameters           []ParamDetail `json:"parameters"`
}

// ParamDetail is one parameter, with the form input its type suggests.
type ParamDetail struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Sensitive   bool            `json:"sensitive"`
	Input       ParamInput      `json:"input"`
}

// Detail summarises task for the API.
func Detail(task Task) TaskDetail {
	d := TaskDetail{
		Name:                 task.Name,
		Environment:          task.Environment,
		Description:          task.Metadata.Description,
		SupportsNoop:         task.Metadata.SupportsNoop,
		Private:              task.Metadata.Private,
		AcceptsAnyParameters: task.Metadata.Parameters == nil,
		Parameters:           []ParamDetail{},
	}
	for name, p := range task.Metadata.Parameters {
		typ := p.Type
		if typ == "" {
			typ = "Any"
		}
		d.Parameters = append(d.Parameters, ParamDetail{
			Name:        name,
			Type:        typ,
			Description: p.Description,
			Default:     p.Default,
			Sensitive:   p.Sensitive,
			Input:       InputFor(p),
		})
	}
	// openvoxserver has already lost the metadata file's own order, so
	// sort for a stable form.
	sort.Slice(d.Parameters, func(i, j int) bool { return d.Parameters[i].Name < d.Parameters[j].Name })
	return d
}

func (h *Handlers) getTask(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("module")
	if t := r.PathValue("task"); t != "init" {
		name += "::" + t
	}
	// A task the caller may not run is answered as one that does not
	// exist, before the catalogue is read, so the two cannot be told apart.
	scope, err := h.taskScope(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !scope.AllowsAnywhere(name) {
		writeError(w, &NotFoundError{Kind: "task", Name: name})
		return
	}
	task, err := h.client.Task(r.Context(), environmentParam(r), name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Detail(task))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, HTTPStatus(err), map[string]string{"error": err.Error()})
}
