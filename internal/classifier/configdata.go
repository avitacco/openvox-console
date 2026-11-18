package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Limits on a group's configuration data. They keep one group from making
// every catalog compile slow: the whole merged map is fetched on each one.
const (
	MaxConfigDataKeys       = 1000
	MaxConfigDataKeyLength  = 255
	MaxConfigDataValueBytes = 64 * 1024

	// lookupOptionsKey is the key Hiera reads merge directives from.
	// Letting it through would silently turn a value into a merge
	// strategy, which this feature does not offer.
	lookupOptionsKey = "lookup_options"
)

var configDataKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_:.\-]+$`)

// ConfigDataError says which key of a group's configuration data was
// refused, and why. Key is empty for a problem with the data as a whole.
type ConfigDataError struct {
	Key    string
	Reason string
}

func (e *ConfigDataError) Error() string {
	if e.Key == "" {
		return "configuration data: " + e.Reason
	}
	return fmt.Sprintf("configuration data key %q: %s", e.Key, e.Reason)
}

// ValidateConfigData checks a group's configuration data. It returns a
// *ConfigDataError for the first problem, taking keys in sorted order so
// the same data always gives the same answer.
func ValidateConfigData(data map[string]any) error {
	if len(data) > MaxConfigDataKeys {
		return &ConfigDataError{Reason: fmt.Sprintf("at most %d keys are allowed", MaxConfigDataKeys)}
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case k == "":
			return &ConfigDataError{Key: k, Reason: "a key cannot be empty"}
		case len(k) > MaxConfigDataKeyLength:
			return &ConfigDataError{Key: k[:32] + "...", Reason: fmt.Sprintf("a key is at most %d characters", MaxConfigDataKeyLength)}
		case k == lookupOptionsKey:
			return &ConfigDataError{Key: k, Reason: "this key is reserved for Hiera's merge options, which are not supported"}
		case !configDataKeyPattern.MatchString(k):
			return &ConfigDataError{Key: k, Reason: "a key may contain only letters, digits and the characters _ : . -"}
		}
		encoded, err := json.Marshal(data[k])
		if err != nil {
			return &ConfigDataError{Key: k, Reason: "the value cannot be stored as JSON"}
		}
		if len(encoded) > MaxConfigDataValueBytes {
			return &ConfigDataError{Key: k, Reason: fmt.Sprintf("a value is at most %d KiB", MaxConfigDataValueBytes/1024)}
		}
	}
	return nil
}

func insertConfigData(ctx context.Context, q queryer, groupID int64, data map[string]any) error {
	for key, value := range data {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode config data %q: %w", key, err)
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO node_group_config_data (group_id, key, value) VALUES ($1, $2, $3)`,
			groupID, key, encoded); err != nil {
			return fmt.Errorf("insert config data %q: %w", key, err)
		}
	}
	return nil
}

func loadConfigData(ctx context.Context, q queryer, groupID int64) (map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT key, value FROM node_group_config_data WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, fmt.Errorf("load config data: %w", err)
	}
	defer rows.Close()
	data := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("load config data: %w", err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode config data %q: %w", key, err)
		}
		data[key] = value
	}
	return data, rows.Err()
}

// ConfigDataChange lists the keys a save added, changed and removed, in
// sorted order. It holds no value, so it is safe to record in an audit
// log (see the config-data spec).
type ConfigDataChange struct {
	Added   []string `json:"added,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Empty reports whether nothing changed.
func (c ConfigDataChange) Empty() bool {
	return len(c.Added) == 0 && len(c.Changed) == 0 && len(c.Removed) == 0
}

// DiffConfigData compares two groups' data by key. Values are compared by
// their JSON encoding, which is how they are stored.
func DiffConfigData(before, after map[string]any) ConfigDataChange {
	var c ConfigDataChange
	for k, v := range after {
		old, ok := before[k]
		if !ok {
			c.Added = append(c.Added, k)
			continue
		}
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(v)
		if string(a) != string(b) {
			c.Changed = append(c.Changed, k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			c.Removed = append(c.Removed, k)
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Changed)
	sort.Strings(c.Removed)
	return c
}
