package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
)

// seedConsole creates the records the console owns itself - node groups,
// roles, users and service tokens - through its own HTTP API.
//
// Every step converges: an existing record is updated or left alone
// rather than duplicated, so running the seed twice leaves the console in
// the same state as running it once.
func seedConsole(ctx context.Context, opts options) error {
	client, err := newConsoleClient(ctx, opts)
	if err != nil {
		return err
	}

	if err := seedGroups(ctx, client); err != nil {
		return err
	}
	roleIDs, err := seedRoles(ctx, client)
	if err != nil {
		return err
	}
	if err := seedUsers(ctx, client, roleIDs); err != nil {
		return err
	}
	return seedServiceTokens(ctx, client)
}

// seedGroups creates the demo node groups, updating one that already
// exists so an edited demo group converges back to its defined state.
func seedGroups(ctx context.Context, client *consoleClient) error {
	// Unlike the other list endpoints, groups is paginated - it answers
	// {items, page, pageSize, total} with a default page size of 25. A
	// page size large enough to hold everything keeps this a single
	// request; the count below catches the day that stops being true.
	var existing struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/groups?pageSize=500", nil, &existing); err != nil {
		return fmt.Errorf("listing groups: %w", err)
	}
	if existing.Total > len(existing.Items) {
		return fmt.Errorf("console reports %d groups but returned %d - the seed's single-page listing is no longer sufficient", existing.Total, len(existing.Items))
	}
	byName := map[string]int64{}
	for _, g := range existing.Items {
		byName[g.Name] = g.ID
	}

	for _, group := range demodata.Groups {
		body := groupBody(group)

		if id, ok := byName[group.Name]; ok {
			if err := client.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/groups/%d", id), body, nil); err != nil {
				return fmt.Errorf("updating group %q: %w", group.Name, err)
			}
			continue
		}
		if err := client.do(ctx, http.MethodPost, "/api/v1/groups", body, nil); err != nil {
			return fmt.Errorf("creating group %q: %w", group.Name, err)
		}
	}

	fmt.Printf("    %d node groups\n", len(demodata.Groups))
	return nil
}

// groupBody renders a demo group as the classifier's wire shape.
func groupBody(group demodata.Group) map[string]any {
	classes := make([]map[string]any, 0, len(group.Classes))
	for _, c := range group.Classes {
		params := c.Parameters
		if params == nil {
			params = map[string]any{}
		}
		classes = append(classes, map[string]any{"name": c.Name, "parameters": params})
	}

	rule := make([]map[string]any, 0, len(group.Rule))
	for _, c := range group.Rule {
		rule = append(rule, map[string]any{
			"factPath": c.FactPath,
			"operator": c.Operator,
			"value":    c.Value,
		})
	}

	params := group.Parameters
	if params == nil {
		params = map[string]any{}
	}

	return map[string]any{
		"name":        group.Name,
		"environment": group.Environment,
		"priority":    group.Priority,
		"classes":     classes,
		"parameters":  params,
		"rule":        rule,
		"pins":        []string{},
	}
}

// seedRoles creates the demo roles and returns their ids by name, so
// users can be assigned to them afterwards.
func seedRoles(ctx context.Context, client *consoleClient) (map[string]int64, error) {
	var existing []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/roles", nil, &existing); err != nil {
		return nil, fmt.Errorf("listing roles: %w", err)
	}
	ids := map[string]int64{}
	for _, r := range existing {
		ids[r.Name] = r.ID
	}

	for _, role := range demodata.Roles {
		body := map[string]any{"name": role.Name, "permissions": role.Permissions}

		if id, ok := ids[role.Name]; ok {
			if err := client.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/roles/%d", id), body, nil); err != nil {
				return nil, fmt.Errorf("updating role %q: %w", role.Name, err)
			}
			continue
		}

		var created struct {
			ID int64 `json:"id"`
		}
		if err := client.do(ctx, http.MethodPost, "/api/v1/roles", body, &created); err != nil {
			return nil, fmt.Errorf("creating role %q: %w", role.Name, err)
		}
		ids[role.Name] = created.ID
	}

	fmt.Printf("    %d roles\n", len(demodata.Roles))
	return ids, nil
}

// seedUsers creates the demo people and assigns their roles.
func seedUsers(ctx context.Context, client *consoleClient, roleIDs map[string]int64) error {
	var existing []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/users", nil, &existing); err != nil {
		return fmt.Errorf("listing users: %w", err)
	}
	ids := map[string]int64{}
	for _, u := range existing {
		ids[u.Username] = u.ID
	}

	for _, user := range demodata.Users {
		body := map[string]any{
			"username":  user.Username,
			"password":  demodata.DemoPassword,
			"firstName": user.FirstName,
			"lastName":  user.LastName,
			"email":     user.Email,
		}

		id, ok := ids[user.Username]
		if ok {
			if err := client.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", id), body, nil); err != nil {
				return fmt.Errorf("updating user %q: %w", user.Username, err)
			}
		} else {
			var created struct {
				ID int64 `json:"id"`
			}
			if err := client.do(ctx, http.MethodPost, "/api/v1/users", body, &created); err != nil {
				return fmt.Errorf("creating user %q: %w", user.Username, err)
			}
			id = created.ID
		}

		for _, roleName := range user.Roles {
			roleID, ok := roleIDs[roleName]
			if !ok {
				return fmt.Errorf("user %q wants role %q, which was not created", user.Username, roleName)
			}
			// Assigning a role the user already holds is the converging
			// case, not an error.
			err := client.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", id, roleID), nil, nil)
			if err != nil && !isConflict(err) {
				return fmt.Errorf("assigning %q to %q: %w", roleName, user.Username, err)
			}
		}
	}

	fmt.Printf("    %d users\n", len(demodata.Users))
	return nil
}

// seedServiceTokens creates the demo service tokens.
//
// The issued token value is deliberately discarded. It is a real
// credential for this console, it is the one thing on the page that must
// never reach a screenshot, and the seed has no use for it.
func seedServiceTokens(ctx context.Context, client *consoleClient) error {
	var existing []struct {
		Name string `json:"name"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/v1/service-tokens", nil, &existing); err != nil {
		return fmt.Errorf("listing service tokens: %w", err)
	}
	have := map[string]bool{}
	for _, t := range existing {
		have[t.Name] = true
	}

	var created int
	for _, token := range demodata.ServiceTokens {
		if have[token.Name] {
			continue
		}
		body := map[string]any{"name": token.Name, "permissions": token.Permissions}
		if err := client.do(ctx, http.MethodPost, "/api/v1/service-tokens", body, nil); err != nil {
			return fmt.Errorf("creating service token %q: %w", token.Name, err)
		}
		created++
	}

	fmt.Printf("    %d service tokens (%d newly issued)\n", len(demodata.ServiceTokens), created)
	return nil
}
