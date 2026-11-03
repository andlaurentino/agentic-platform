package llms

import (
	"encoding/json"
	"strings"

	"google.golang.org/genai"
)

// Helpers shared by the provider adapters.

// declarationSchema returns the tool's input JSON Schema as a generic map.
func declarationSchema(fd *genai.FunctionDeclaration) (map[string]any, error) {
	var src any = map[string]any{"type": "object"}
	switch {
	case fd.ParametersJsonSchema != nil:
		src = fd.ParametersJsonSchema
	case fd.Parameters != nil:
		src = genaiSchemaToJSON(fd.Parameters)
	}
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// genaiSchemaToJSON converts the OpenAPI-style genai.Schema to JSON Schema.
func genaiSchemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	m := map[string]any{}
	if s.Type != "" {
		t := strings.ToLower(string(s.Type))
		if s.Nullable != nil && *s.Nullable {
			m["type"] = []string{t, "null"}
		} else {
			m["type"] = t
		}
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for k, v := range s.Properties {
			props[k] = genaiSchemaToJSON(v)
		}
		m["properties"] = props
	}
	if s.Items != nil {
		m["items"] = genaiSchemaToJSON(s.Items)
	}
	if len(s.AnyOf) > 0 {
		var anyOf []any
		for _, v := range s.AnyOf {
			anyOf = append(anyOf, genaiSchemaToJSON(v))
		}
		m["anyOf"] = anyOf
	}
	return m
}

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var parts []string
	for _, p := range c.Parts {
		if p != nil && p.Text != "" && !p.Thought {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}
