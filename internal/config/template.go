package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed templates/*.json
var templateFS embed.FS

// LoadTemplate reads a built-in template by name (without .json extension),
// applies variable substitutions, and returns the resulting TeamConfig.
func LoadTemplate(name string, vars map[string]string) (*TeamConfig, error) {
	filename := name + ".json"
	data, err := templateFS.ReadFile("templates/" + filename)
	if err != nil {
		return nil, fmt.Errorf("template %q not found (available: %v)", name, ListTemplates())
	}

	// Apply variable substitutions.
	content := string(data)
	for k, v := range vars {
		content = strings.ReplaceAll(content, "${"+k+"}", v)
	}

	var tc TeamConfig
	if err := json.Unmarshal([]byte(content), &tc); err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}

	return &tc, nil
}

// ListTemplates returns the names of all built-in templates (without .json extension).
func ListTemplates() []string {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return names
}

// LoadConfigOrTemplate loads a team config from either a file path (--config)
// or a built-in template (--team), with variable substitution from vars.
// Exactly one of configPath or teamName should be non-empty.
func LoadConfigOrTemplate(configPath, teamName string, vars SetVars) (*TeamConfig, error) {
	if configPath != "" && teamName != "" {
		return nil, fmt.Errorf("--config and --team are mutually exclusive")
	}
	if configPath == "" && teamName == "" {
		available := ListTemplates()
		return nil, fmt.Errorf("either --config or --team is required (available templates: %s)",
			strings.Join(available, ", "))
	}

	if teamName != "" {
		return LoadTemplate(teamName, vars.All)
	}
	tc, err := LoadOrConvert(configPath)
	if err != nil {
		return nil, err
	}
	// Apply explicit --set overrides to file-based configs before validation.
	if vars.Explicit["repo"] {
		tc.Repo = vars.All["repo"]
	}
	if vars.Explicit["prompt"] && vars.All["prompt"] != "" {
		tc.Prompt = vars.All["prompt"]
	}
	if err := tc.Validate(); err != nil {
		return nil, err
	}
	return tc, nil
}

// SetVars holds parsed --set flag values and tracks which were explicitly provided.
type SetVars struct {
	All      map[string]string
	Explicit map[string]bool
}

// ParseSetFlags parses a slice of "key=value" strings into a SetVars.
func ParseSetFlags(flags []string) SetVars {
	sv := SetVars{
		All:      make(map[string]string),
		Explicit: make(map[string]bool),
	}
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) == 2 {
			sv.All[parts[0]] = parts[1]
			sv.Explicit[parts[0]] = true
		}
	}
	// Ensure repo and prompt have defaults even if not set.
	if _, ok := sv.All["repo"]; !ok {
		if wd, err := filepath.Abs("."); err == nil {
			sv.All["repo"] = wd
		}
	}
	if _, ok := sv.All["prompt"]; !ok {
		sv.All["prompt"] = ""
	}
	return sv
}
