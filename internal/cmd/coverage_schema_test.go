package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// PostgreSQL оборачивает Storage/Pooler/Maintenance в allOf. Потеря обхода
// делала зелёной сверку без вложенных полей и всех enum, независимо от запусков CLI.
func TestAPICoverageTraversesAllOf(t *testing.T) {
	t.Parallel()

	s := covSpec{doc: map[string]any{"components": map[string]any{"schemas": map[string]any{
		"Mode": map[string]any{"properties": map[string]any{"mode": map[string]any{"type": "string", "enum": []any{"STANDARD", "BUSINESS"}}}},
	}}}}
	schema := map[string]any{"allOf": []any{map[string]any{"properties": map[string]any{
		"requests": map[string]any{"type": "array", "items": map[string]any{"allOf": []any{map[string]any{"$ref": "#/components/schemas/Mode"}}}},
	}}}}
	assert.ElementsMatch(t, []covField{{path: "requests"}, {path: "requests.mode", enum: []string{"STANDARD", "BUSINESS"}}}, s.fields("", schema))
}
