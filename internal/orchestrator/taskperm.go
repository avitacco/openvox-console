package orchestrator

import (
	"fmt"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// TaskNotPermittedError is a task the caller's roles do not allow them to
// run: none of their grants names it. Nothing is created for it.
//
// It is decided from the task's name alone, before the catalogue is read,
// so that a task the caller may not run and one that does not exist are
// refused identically (see design.md, "The check sits in Starter").
type TaskNotPermittedError struct {
	Task string
}

func (e *TaskNotPermittedError) Error() string {
	return fmt.Sprintf("you are not allowed to run the task %q", e.Task)
}

// checkTaskAllowed refuses task unless some grant allows it by name.
func checkTaskAllowed(tasks taskscope.Scope, task string) error {
	if !tasks.Allows(task) {
		return &TaskNotPermittedError{Task: task}
	}
	return nil
}

// taskNodes applies the nodes tasks may be run on to a resolved target.
//
// resolved is what the target gave, already limited to the caller's
// orchestrator:run scope. A node list - source nil - is the caller naming
// nodes, and naming one the task may not run on is refused, as naming a
// node outside the caller's groups is. A query or group is the console
// working nodes out for them, and is narrowed, as it already is to their
// groups.
//
// allowed is the nodes every task seen so far may run on, which a plan
// carries from step to step so that all its steps still run on the same
// nodes. The returned scope is allowed with this task's nodes applied.
func taskNodes(tasks taskscope.Scope, task string, resolved []string, listed bool, allowed nodescope.Scope) (nodescope.Scope, error) {
	nodes := tasks.NodesFor(task)
	if listed {
		if outside := nodes.Outside(resolved); len(outside) > 0 {
			return allowed, &OutOfScopeError{Outside: outside, Task: task}
		}
	}
	return allowed.Within(nodes), nil
}
