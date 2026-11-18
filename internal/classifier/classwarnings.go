package classifier

import "context"

// DefaultEnvironment is the environment Puppet uses for a node that
// nothing assigns one.
const DefaultEnvironment = "production"

// ClassWarning says that one of a group's classes does not match the
// code of the group's environment: the class is not defined there, a
// parameter is not one the class declares, or a value does not fit the
// declared type. Kind, and the wording of Detail, are the class
// catalogue's (see internal/classcatalog).
//
// It is advice only. A group saves with it, and classification neither
// reads it nor changes because of it.
type ClassWarning struct {
	Kind      string `json:"kind"`
	Class     string `json:"class"`
	Parameter string `json:"parameter,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// ClassChecker compares classes with what environment's code defines.
// checked is false when that could not be found out - discovery is
// switched off, refused or unreachable - in which case there are no
// warnings to give, which is not the same as there being nothing wrong.
type ClassChecker func(ctx context.Context, environment string, classes []Class) (warnings []ClassWarning, checked bool)

// SetClassChecker makes group responses carry class warnings worked out
// by check. Without one, no group is ever checked.
func (h *Handlers) SetClassChecker(check ClassChecker) {
	h.checkClasses = check
}

// EffectiveEnvironment is the environment a group's classes are looked
// up in: its own, else its nearest ancestor's that sets one, else
// production. groups is every group; it may be nil, in which case only
// the group's own setting can be seen.
//
// This is the environment a node in the group gets when no other group
// it matches says otherwise - which a group, on its own, cannot know.
func EffectiveEnvironment(groups []Group, g Group) string {
	byID := make(map[int64]Group, len(groups))
	for _, other := range groups {
		byID[other.ID] = other
	}
	// Bounded by the number of groups, so a cycle in stored data (which
	// the store refuses to create) could not loop here.
	for hops := 0; hops <= len(groups); hops++ {
		if g.Environment != nil && *g.Environment != "" {
			return *g.Environment
		}
		if g.ParentID == nil {
			break
		}
		parent, ok := byID[*g.ParentID]
		if !ok {
			break
		}
		g = parent
	}
	return DefaultEnvironment
}

// withClassWarnings fills in the class fields of resp. It never fails
// the request it decorates.
func (h *Handlers) withClassWarnings(ctx context.Context, all []Group, resp groupResponse) groupResponse {
	resp.ClassEnvironment = EffectiveEnvironment(all, resp.Group)
	resp.ClassWarnings = []ClassWarning{}
	if h.checkClasses == nil {
		return resp
	}
	// A group with no classes has nothing to check, and is not worth a
	// read of the server.
	if len(resp.Classes) == 0 {
		return resp
	}
	warnings, checked := h.checkClasses(ctx, resp.ClassEnvironment, resp.Classes)
	resp.ClassesChecked = checked
	if warnings != nil {
		resp.ClassWarnings = warnings
	}
	return resp
}
