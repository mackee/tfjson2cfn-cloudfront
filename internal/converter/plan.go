package converter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// plan is the subset of `terraform show -json` output we consume. Values are
// already resolved by Terraform; the configuration section is consulted only to
// recover cross-resource references (which are "known after apply" and so are
// absent from planned_values).
type plan struct {
	FormatVersion   string `json:"format_version"`
	ResourceChanges []struct {
		Address string `json:"address"`
		Change  struct {
			AfterUnknown map[string]any `json:"after_unknown"`
		} `json:"change"`
	} `json:"resource_changes"`
	PlannedValues struct {
		RootModule module `json:"root_module"`
	} `json:"planned_values"`
	Configuration struct {
		RootModule configModule `json:"root_module"`
	} `json:"configuration"`
}

type module struct {
	Resources    []plannedResource `json:"resources"`
	ChildModules []json.RawMessage `json:"child_modules"`
}

type plannedResource struct {
	Address string         `json:"address"`
	Mode    string         `json:"mode"`
	Type    string         `json:"type"`
	Name    string         `json:"name"`
	Values  map[string]any `json:"values"`
}

type configModule struct {
	Resources    []configResource  `json:"resources"`
	ChildModules []json.RawMessage `json:"child_modules"`
}

type configResource struct {
	Address     string         `json:"address"`
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Expressions map[string]any `json:"expressions"`
}

func parsePlan(data []byte) (*plan, error) {
	var p plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse terraform plan json: %w", err)
	}
	if p.FormatVersion == "" {
		return nil, fmt.Errorf("input does not look like `terraform show -json` output (no format_version)")
	}
	return &p, nil
}

// configFor returns the configuration expressions for a resource address, or
// nil when absent. The expressions tell us which attributes are references.
//
// planned_values addresses carry the count/for_each index ("type.name[0]")
// while configuration addresses do not ("type.name"), so the index is stripped
// before matching.
func (p *plan) configFor(address string) map[string]any {
	base := address
	if i := strings.IndexByte(base, '['); i >= 0 {
		base = base[:i]
	}
	for _, r := range p.Configuration.RootModule.Resources {
		if r.Address == base {
			return r.Expressions
		}
	}
	return nil
}

// --- typed accessors over the dynamic planned_values maps ---

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolVal(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

// numPtr returns the numeric value at key, or nil when absent/null. Terraform
// numbers arrive as float64 via encoding/json.
func numPtr(m map[string]any, key string) *float64 {
	if v, ok := m[key].(float64); ok {
		return &v
	}
	return nil
}

func num(m map[string]any, key string) float64 {
	if p := numPtr(m, key); p != nil {
		return *p
	}
	return 0
}

// strList returns a []string from a JSON array, or nil.
func strList(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// blocks returns a Terraform nested-block list as []map[string]any. Single
// (MaxItems 1) blocks arrive as a one-element list too.
func blocks(m map[string]any, key string) []map[string]any {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		if bm, ok := e.(map[string]any); ok {
			out = append(out, bm)
		}
	}
	return out
}

// firstBlock returns the single element of a MaxItems-1 block, or nil.
func firstBlock(m map[string]any, key string) map[string]any {
	bs := blocks(m, key)
	if len(bs) == 0 {
		return nil
	}
	return bs[0]
}
