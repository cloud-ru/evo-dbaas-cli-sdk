package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func load(t *testing.T, s string) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(s), &doc))

	return doc
}

func mustFix(t *testing.T, doc map[string]any) (map[string]any, []string) {
	t.Helper()

	doc, log, err := fix(doc)
	require.NoError(t, err)

	return doc, log
}

func dig(t *testing.T, v any, keys ...string) any {
	t.Helper()

	for _, k := range keys {
		m, ok := v.(map[string]any)
		require.Truef(t, ok, "%s: не map", k)
		v = m[k]
	}

	return v
}

func TestSwagger2Leftovers(t *testing.T) {
	doc, log := mustFix(t, load(t, `
swagger: "2.0"
openapi: 3.0.0
consumes: [application/json]
produces: [application/json]
definitions:
  A: {type: object}
  B: {allOf: [{$ref: '#/definitions/A'}]}
paths:
  /a:
    get:
      responses:
        "200": {content: {application/json: {schema: {$ref: '#/definitions/A'}}}}
        "201": {description: ok, schema: {$ref: '#/definitions/B'}}
`))
	assert.NotContains(t, doc, "swagger")
	assert.NotContains(t, doc, "consumes")
	assert.NotContains(t, doc, "produces")
	assert.NotContains(t, doc, "definitions")
	assert.Equal(t, map[string]any{"type": "object"}, dig(t, doc, "components", "schemas", "A"))
	assert.Equal(t, []any{map[string]any{"$ref": "#/components/schemas/A"}}, dig(t, doc, "components", "schemas", "B", "allOf"))

	rs := dig(t, doc, "paths", "/a", "get", "responses")
	assert.Equal(t, "#/components/schemas/A", dig(t, rs, "200", "content", "application/json", "schema", "$ref"))
	assert.Equal(t, map[string]any{
		"description": "ok",
		"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/B"}}},
	}, dig(t, rs, "201"))
	assert.Equal(t, []string{
		"definitions → components.schemas: 2",
		"ссылки #/definitions/ → #/components/schemas/: 3",
		"schema ответа → content: 1",
	}, log)
}

func TestParamsAndBody(t *testing.T) {
	doc, log := mustFix(t, load(t, `
openapi: 3.0.0
components: {schemas: {B: {type: object}}}
paths:
  /a:
    post:
      parameters:
        - {in: query, name: filter, type: string}
        - {in: query, name: pageSize, type: string, format: uint64}
        - {in: query, name: empty}
        - {in: query, name: view, type: string, enum: [BASIC, FULL], default: BASIC}
        - {in: query, name: ids, type: array, items: {type: string}}
        - {in: path, name: id, schema: {type: string}}
        - {in: header, name: h, content: {text/plain: {schema: {type: string}}}}
        - {$ref: '#/components/parameters/P'}
        - {in: body, name: body, required: true, schema: {$ref: '#/components/schemas/B'}}
  /b:
    put:
      parameters:
        - {in: body, name: body, description: тело, x-go-name: Body, schema: {type: object}}
`))
	ps := dig(t, doc, "paths", "/a", "post", "parameters").([]any)
	require.Len(t, ps, 8)
	assert.Equal(t, map[string]any{"in": "query", "name": "filter", "schema": map[string]any{"type": "string"}}, ps[0])
	assert.Equal(t, map[string]any{"type": "string", "format": "uint64"}, dig(t, ps[1], "schema"))
	assert.Equal(t, map[string]any{"type": "string"}, dig(t, ps[2], "schema"))
	assert.Equal(t, map[string]any{"in": "query", "name": "view", "schema": map[string]any{
		"type": "string", "enum": []any{"BASIC", "FULL"}, "default": "BASIC",
	}}, ps[3])
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, dig(t, ps[4], "schema"))
	assert.Equal(t, map[string]any{"type": "string"}, dig(t, ps[5], "schema"))
	assert.NotContains(t, ps[6], "schema")
	assert.Equal(t, map[string]any{"$ref": "#/components/parameters/P"}, ps[7])

	rb := dig(t, doc, "paths", "/a", "post", "requestBody")
	assert.Equal(t, true, dig(t, rb, "required"))
	assert.Equal(t, "#/components/schemas/B", dig(t, rb, "content", "application/json", "schema", "$ref"))

	assert.Empty(t, dig(t, doc, "paths", "/b", "put", "parameters"))
	assert.Equal(t, map[string]any{
		"required":    false,
		"description": "тело",
		"x-go-name":   "Body",
		"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}},
	}, dig(t, doc, "paths", "/b", "put", "requestBody"))
	assert.Equal(t, []string{"параметры без schema: 5", "in: body → requestBody: 2"}, log)
}

func TestBodyErrors(t *testing.T) {
	for name, params := range map[string]string{
		"два тела":        `[{in: body, name: a, schema: {type: object}}, {in: body, name: b, schema: {type: object}}]`,
		"тело без schema": `[{in: body, name: a}]`,
		"рядом с requestBody": `[{in: body, name: a, schema: {type: object}}]
      requestBody: {content: {application/json: {schema: {type: object}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := fix(load(t, "paths:\n  /a:\n    post:\n      parameters: "+params+"\n"))
			require.ErrorContains(t, err, "post /a")
		})
	}
}

func TestNullableAnyOf(t *testing.T) {
	doc, log := mustFix(t, load(t, `
openapi: 3.1.0
components:
  schemas:
    A: {type: object}
    Vm:
      properties:
        ref: {description: ссылка, anyOf: [{$ref: '#/components/schemas/A'}, {type: null}]}
        str: {anyOf: [{type: 'null'}, {type: string, description: из X}], description: своё}
        union: {anyOf: [{type: string}, {type: integer}]}
        three: {anyOf: [{type: string}, {type: integer}, {type: null}]}
`))
	props := dig(t, doc, "components", "schemas", "Vm", "properties")
	assert.Equal(t, map[string]any{"description": "ссылка", "$ref": "#/components/schemas/A", "nullable": true}, dig(t, props, "ref"))
	assert.Equal(t, map[string]any{"description": "своё", "type": "string", "nullable": true}, dig(t, props, "str"))
	assert.Len(t, dig(t, props, "union", "anyOf"), 2)
	assert.Len(t, dig(t, props, "three", "anyOf"), 3)
	assert.Equal(t, []string{"anyOf [X, null] → X, nullable: 2"}, log)
}

func TestDanglingSchema(t *testing.T) {
	doc, log := mustFix(t, load(t, `
openapi: 3.0.0
components:
  schemas:
    Vm:
      properties:
        ip: {$ref: '#/components/schemas/Missing'}
        list: {type: array, items: {allOf: [{$ref: '#/components/schemas/Gone'}]}}
        ok: {$ref: '#/components/schemas/Vm'}
`))
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": true},
		dig(t, doc, "components", "schemas", "Missing"))
	assert.Equal(t, []string{"несуществующие схемы → объект с любыми полями: [Gone Missing]"}, log)
}

func TestCleanSpecUntouched(t *testing.T) {
	const s = `
openapi: 3.0.0
components: {schemas: {A: {type: object}}}
paths:
  /a:
    get:
      parameters: [{in: query, name: q, schema: {type: string}}]
      responses: {"200": {content: {application/json: {schema: {$ref: '#/components/schemas/A'}}}}}
`

	doc, log := mustFix(t, load(t, s))
	assert.Empty(t, log)
	assert.Equal(t, load(t, s), doc)
}
