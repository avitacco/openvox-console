// Package agentapi lets AI agents use the console through MCP, the Model
// Context Protocol, served over streamable HTTP at /mcp.
//
// An agent authenticates with an agent token (see internal/rbac). What it
// reads, it reads through the console's own endpoints with that token, so
// permissions, node scope and masking are the ones every other caller gets.
// What it asks to change is stored as a request for a person to approve
// (internal/agentrequests); no tool starts anything itself.
package agentapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/agentrequests"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
)

// Limits on what the endpoint accepts and returns.
const (
	// MaxBodyBytes is the largest request body accepted.
	MaxBodyBytes = 256 << 10
	// MaxResultBytes is the most a tool returns; more is cut, with a note
	// saying so and how to ask for the rest.
	MaxResultBytes = 64 << 10
	// DefaultRatePerMinute is the calls a minute one token may make.
	DefaultRatePerMinute = 60
)

// supported protocol versions, newest first.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Config is what Handler needs.
type Config struct {
	Verifier *rbac.Verifier
	Requests *agentrequests.Service
	// Caller says what an authenticated agent token may reach: its node
	// scope and the tasks it may run, as the console works them out for
	// any other caller.
	Caller func(r *http.Request, claims *rbac.Claims) (agentrequests.Caller, error)
	// RatePerMinute limits each token's calls; zero is the default.
	RatePerMinute int
	Version       string
	Logger        *slog.Logger
}

// Handler serves /mcp.
type Handler struct {
	cfg      Config
	limiter  *limiter
	internal http.Handler
	tools    []tool
	byName   map[string]*tool
}

// New builds a Handler. Until SetInternal is called, read tools answer that
// they are unavailable.
func New(cfg Config) *Handler {
	if cfg.RatePerMinute <= 0 {
		cfg.RatePerMinute = DefaultRatePerMinute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	h := &Handler{cfg: cfg, limiter: newLimiter(cfg.RatePerMinute)}
	h.tools = buildTools()
	h.byName = make(map[string]*tool, len(h.tools))
	for i := range h.tools {
		h.byName[h.tools[i].name] = &h.tools[i]
	}
	return h
}

// SetInternal gives the handler the console's own router, which read tools
// call in process with the agent's token. It is the same router that serves
// the REST API, so every read is checked, scoped and masked as one.
func (h *Handler) SetInternal(router http.Handler) { h.internal = router }

// Register wires the endpoint onto mux. Only a console started with the
// endpoint enabled calls it.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /mcp", h.serve)
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "POST")
		http.Error(w, "this endpoint takes POSTed JSON-RPC messages", http.StatusMethodNotAllowed)
	})
}

// JSON-RPC.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errNoMethod       = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
	errRateLimited    = -32000
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func rpcFail(id json.RawMessage, code int, message string) rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

// sameOrigin refuses a browser page on another origin: a request with an
// Origin header must name this host. Agents are not browsers and send none.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		unauthorized(w, "an agent token is required")
		return
	}
	claims, err := h.cfg.Verifier.VerifyAgent(token)
	if err != nil {
		unauthorized(w, "invalid, expired or revoked agent token")
		return
	}
	if ok, wait := h.limiter.allow(claims.ID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		writeJSON(w, http.StatusTooManyRequests, rpcFail(nil, errRateLimited, fmt.Sprintf("rate limit exceeded; retry in %d seconds", int(wait.Seconds()))))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, rpcFail(nil, errInvalidRequest, fmt.Sprintf("the request is larger than %d KiB", MaxBodyBytes>>10)))
			return
		}
		writeJSON(w, http.StatusBadRequest, rpcFail(nil, errParse, "could not read the request"))
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			writeJSON(w, http.StatusBadRequest, rpcFail(nil, errParse, "invalid JSON"))
			return
		}
		var out []rpcResponse
		for _, raw := range batch {
			if resp, ok := h.dispatch(r, claims, raw); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	resp, ok := h.dispatch(r, claims, trimmed)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="openvox-console-agent"`)
	writeJSON(w, http.StatusUnauthorized, rpcFail(nil, errInvalidRequest, message))
}

// dispatch answers one message. A notification has no answer.
func (h *Handler) dispatch(r *http.Request, claims *rbac.Claims, raw []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcFail(nil, errParse, "invalid JSON"), true
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return rpcFail(req.ID, errInvalidRequest, "not a JSON-RPC 2.0 request"), true
	}
	notification := len(req.ID) == 0
	if strings.HasPrefix(req.Method, "notifications/") || (notification && req.Method != "ping") {
		return rpcResponse{}, false
	}
	switch req.Method {
	case "initialize":
		return h.initialize(req), true
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": h.listTools(claims)}}, true
	case "tools/call":
		return h.callTool(r, claims, req), true
	default:
		return rpcFail(req.ID, errNoMethod, "method not found: "+req.Method), true
	}
}

func (h *Handler) initialize(req rpcRequest) rpcResponse {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &params)
	version := protocolVersions[0]
	for _, v := range protocolVersions {
		if v == params.ProtocolVersion {
			version = v
		}
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "openvox-console", "version": h.cfg.Version},
		"instructions": "Read tools answer immediately. Tools whose names start with request_ do not run anything: " +
			"they store a request that a person must approve in the console, and return a request id. " +
			"Check it with get_request; once approved it names a job, which get_job reports.",
	}}
}

type toolListing struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// listTools is the tools claims may use: those whose permission the token
// carries.
func (h *Handler) listTools(claims *rbac.Claims) []toolListing {
	out := make([]toolListing, 0, len(h.tools))
	for _, t := range h.tools {
		if !t.allowed(claims) {
			continue
		}
		out = append(out, toolListing{
			Name: t.name, Description: t.description, InputSchema: t.schema(),
			Annotations: map[string]any{"readOnlyHint": t.readOnly, "destructiveHint": false, "openWorldHint": false},
		})
	}
	return out
}

func (h *Handler) callTool(r *http.Request, claims *rbac.Claims, req rpcRequest) rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		return rpcFail(req.ID, errInvalidParams, "tools/call needs a tool name")
	}
	t, ok := h.byName[params.Name]
	if !ok || !t.allowed(claims) {
		// A tool the token may not use does not exist for it.
		return rpcFail(req.ID, errInvalidParams, "unknown tool: "+params.Name)
	}
	args := map[string]any{}
	if len(bytes.TrimSpace(params.Arguments)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(params.Arguments))
		dec.UseNumber() // numbers stay numbers: an Integer is told from a Float
		if err := dec.Decode(&args); err != nil {
			return rpcFail(req.ID, errInvalidParams, "arguments must be an object")
		}
	}
	text, err := t.run(&call{h: h, r: r, claims: claims}, args)
	if err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(err.Error(), true)}
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(truncate(text), false)}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

func truncate(s string) string {
	if len(s) <= MaxResultBytes {
		return s
	}
	cut := s[:MaxResultBytes]
	return cut + fmt.Sprintf("\n\n[The result was cut at %d KiB. Ask for less: use filters, a smaller pageSize, or the next page.]", MaxResultBytes>>10)
}
