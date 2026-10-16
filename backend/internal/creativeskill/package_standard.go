package creativeskill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Standard frontmatter is data. In particular allowed-tools never grants access.
func standardPackage(raw, extension []byte) (Package, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Package{}, fmt.Errorf("%w: SKILL.md needs YAML frontmatter", creativeops.ErrValidation)
	}
	parts := strings.SplitN(text[4:], "\n---\n", 2)
	if len(parts) != 2 {
		return Package{}, fmt.Errorf("%w: unclosed frontmatter", creativeops.ErrValidation)
	}
	var front struct {
		Name          string            `yaml:"name"`
		Description   string            `yaml:"description"`
		License       string            `yaml:"license"`
		Compatibility string            `yaml:"compatibility"`
		Metadata      map[string]string `yaml:"metadata"`
		AllowedTools  string            `yaml:"allowed-tools"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(parts[0]))
	decoder.KnownFields(true)
	if err := decoder.Decode(&front); err != nil {
		return Package{}, fmt.Errorf("%w: frontmatter: %v", creativeops.ErrValidation, err)
	}
	if !validStandardSlug(front.Name) || strings.Contains(front.Name, "--") || strings.HasSuffix(front.Name, "-") || strings.TrimSpace(front.Description) == "" || utf8.RuneCountInString(front.Description) > 1024 || utf8.RuneCountInString(front.Compatibility) > 500 || strings.TrimSpace(parts[1]) == "" {
		return Package{}, fmt.Errorf("%w: invalid name, description, compatibility or instructions", creativeops.ErrValidation)
	}
	var ext struct {
		Version       int      `json:"manifest_schema_version"`
		DisplayName   string   `json:"display_name"`
		RequiredTools []string `json:"required_tools"`
		Capabilities  []string `json:"required_model_capabilities"`
		Guidance      string   `json:"output_guidance"`
	}
	if len(extension) > 0 {
		d := json.NewDecoder(bytes.NewReader(extension))
		d.DisallowUnknownFields()
		if err := d.Decode(&ext); err != nil {
			return Package{}, fmt.Errorf("%w: manifest: %v", creativeops.ErrValidation, err)
		}
	}
	if ext.DisplayName == "" {
		ext.DisplayName = front.Name
	}
	metadata := map[string]any{}
	if front.License != "" {
		metadata["license"] = front.License
	}
	if front.Compatibility != "" {
		metadata["compatibility"] = front.Compatibility
	}
	if front.AllowedTools != "" {
		metadata["allowed-tools"] = front.AllowedTools
	}
	if len(front.Metadata) > 0 {
		metadata["metadata"] = front.Metadata
	}
	m := Manifest{ManifestSchemaVersion: 2, RequiredTools: ext.RequiredTools, RequiredModelCapabilities: ext.Capabilities, OutputGuidance: ext.Guidance, Metadata: metadata}
	if err := m.validate(); err != nil {
		return Package{}, err
	}
	return Package{Slug: front.Name, DisplayName: ext.DisplayName, Description: front.Description, Instructions: strings.TrimSpace(parts[1]), Manifest: m}, nil
}

func executableResource(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".py", ".sh", ".bash", ".js", ".mjs", ".ts", ".exe", ".wasm":
		return true
	}
	return false
}

func plainTextResource(body []byte) bool {
	for _, r := range string(body) {
		if (r < 32 && r != '\n' && r != '\r' && r != '\t') || r == 127 {
			return false
		}
	}
	return true
}
