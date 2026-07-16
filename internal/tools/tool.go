// Package tools is Talunor's action layer: the capabilities the agent can
// invoke during a ReAct-style act/observe loop.
//
// A Tool is a named, described, schema-typed function. The Registry holds a set
// of them and exposes their Defs to the LLM (as OpenAI-style function tools);
// when the model asks to call one, the agent runs it via the Registry and feeds
// the string result back as an observation.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Tool is a single capability the agent can call.
type Tool interface {
	// Name is the stable identifier the model uses to call the tool
	// (snake_case, no spaces).
	Name() string
	// Description tells the model what the tool does and when to use it.
	Description() string
	// Schema is the JSON Schema (an "object") describing the tool's arguments.
	Schema() json.RawMessage
	// Execute runs the tool with JSON-encoded arguments and returns the result
	// the model will observe. A returned error is surfaced to the model as an
	// observation (so it can retry or recover), not treated as fatal.
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Approvable is an optional interface a Tool may implement to require explicit
// human approval before each call. A tool that returns true is gated by the
// agent's approval step; tools that don't implement it run freely. Use it for
// anything with side effects (e.g. running shell commands).
type Approvable interface {
	RequiresApproval() bool
}

// ApprovableFor is a finer-grained variant of [Approvable]: the tool decides
// per-call, from the arguments, whether approval is needed. When a tool
// implements it the agent consults it instead of Approvable — so, e.g.,
// web_fetch can wave through hosts on a user-configured allowlist while still
// prompting for everything else. It is the first step toward argument-level
// policy (which tool+args are auto-allowed vs. need a human).
type ApprovableFor interface {
	RequiresApprovalForArgs(args json.RawMessage) bool
}

// Def is a tool's public definition, handed to the LLM. It is transport-neutral;
// the provider adapter maps it onto the wire format (OpenAI "function" tools).
type Def struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Registry is a concurrency-safe set of tools keyed by name.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry builds a registry containing ts (later duplicates overwrite
// earlier ones by name).
func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(ts))}
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

// Register adds (or replaces) a tool.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns the tool with the given name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Len reports how many tools are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Defs returns the tool definitions for the LLM, sorted by name for a stable
// prompt (which also keeps provider prompt-caching effective).
func (r *Registry) Defs() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]Def, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, Def{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		})
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// Execute looks up name and runs it, returning the observation the model should
// see. A missing tool or an execution error is returned as an observation string
// (prefixed with "error:") rather than a Go error, so the act/observe loop keeps
// going and the model can react to the failure.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) string {
	t, ok := r.Get(name)
	if !ok {
		return fmt.Sprintf("error: no such tool %q", name)
	}
	out, err := t.Execute(ctx, args)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}
