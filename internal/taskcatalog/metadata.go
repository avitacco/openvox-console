package taskcatalog

import (
	"encoding/json"
	"fmt"
)

// Metadata is a task's metadata file, as openvoxserver returns it.
type Metadata struct {
	Description string
	// Parameters is nil when the metadata has no "parameters" key at all,
	// which Puppet treats as "accepts anything" - unlike an empty object,
	// which accepts nothing.
	Parameters      map[string]Parameter
	InputMethod     string
	Implementations []Implementation
	// Files are supporting files every implementation uses,
	// module-relative ("mod/lib/helper.rb").
	Files        []string
	SupportsNoop bool
	Private      bool
}

// Parameter is one declared task parameter.
type Parameter struct {
	// Type is the Puppet type expression, as written. Empty means Any.
	Type        string          `json:"type,omitempty"`
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Sensitive   bool            `json:"sensitive,omitempty"`
}

// Implementation is one entry of a task's "implementations" list.
type Implementation struct {
	Name         string   `json:"name"`
	Requirements []string `json:"requirements,omitempty"`
	InputMethod  string   `json:"input_method,omitempty"`
	Files        []string `json:"files,omitempty"`
}

var validInputMethods = map[string]bool{"": true, "stdin": true, "environment": true, "powershell": true, "both": true}

// ParseMetadata decodes a task's metadata object. An empty or null
// document is valid - a task with no metadata file at all - and yields
// zero Metadata with nil Parameters.
func ParseMetadata(raw json.RawMessage) (Metadata, error) {
	var doc struct {
		Description     string                `json:"description"`
		Parameters      *map[string]Parameter `json:"parameters"`
		InputMethod     string                `json:"input_method"`
		Implementations []Implementation      `json:"implementations"`
		Files           []string              `json:"files"`
		SupportsNoop    bool                  `json:"supports_noop"`
		Private         bool                  `json:"private"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return Metadata{}, fmt.Errorf("decode task metadata: %w", err)
		}
	}
	md := Metadata{
		Description:     doc.Description,
		InputMethod:     doc.InputMethod,
		Implementations: doc.Implementations,
		Files:           doc.Files,
		SupportsNoop:    doc.SupportsNoop,
		Private:         doc.Private,
	}
	if doc.Parameters != nil {
		md.Parameters = *doc.Parameters
		if md.Parameters == nil {
			md.Parameters = map[string]Parameter{}
		}
	}
	if !validInputMethods[md.InputMethod] {
		return Metadata{}, fmt.Errorf("unsupported input_method %q", md.InputMethod)
	}
	for _, impl := range md.Implementations {
		if impl.Name == "" {
			return Metadata{}, fmt.Errorf("an implementation has no name")
		}
		if !validInputMethods[impl.InputMethod] {
			return Metadata{}, fmt.Errorf("implementation %s: unsupported input_method %q", impl.Name, impl.InputMethod)
		}
	}
	return md, nil
}
