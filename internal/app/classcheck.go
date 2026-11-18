package app

import (
	"context"

	"github.com/voxpupuli/enterprise-console/internal/classcatalog"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
)

// checkGroupClasses adapts the class catalogue to what the classifier's
// handlers ask of it, so neither package imports the other.
//
// A failure to read the classes is "not checked", never an error: it
// must not fail the group request it decorates, and must not be shown
// as every class being missing.
func checkGroupClasses(catalog *classcatalog.Catalog) classifier.ClassChecker {
	return func(ctx context.Context, environment string, classes []classifier.Class) ([]classifier.ClassWarning, bool) {
		env, err := catalog.Classes(ctx, environment)
		if err != nil || env.Stale {
			return nil, false
		}
		uses := make([]classcatalog.Use, len(classes))
		for i, c := range classes {
			uses[i] = classcatalog.Use{Name: c.Name, Parameters: c.Parameters}
		}
		found := classcatalog.Check(env, uses)
		warnings := make([]classifier.ClassWarning, len(found))
		for i, w := range found {
			warnings[i] = classifier.ClassWarning{Kind: w.Kind, Class: w.Class, Parameter: w.Parameter, Detail: w.Detail}
		}
		return warnings, true
	}
}
