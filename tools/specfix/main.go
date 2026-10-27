// Command specfix приводит опубликованную спецификацию cloud.ru к OpenAPI 3.0, который понимает oapi-codegen.
// api/ остаётся как скачано; scripts/generate.sh пропускает через specfix compute и iam.
//
//	go run ./tools/specfix in.yaml out.yaml
//
// Правила общие, не под конкретные схемы:
//   - остатки Swagger 2 (iam): definitions → components.schemas, ссылки #/definitions/ → #/components/schemas/,
//     swagger, consumes, produces убираются;
//   - параметр без schema (type, format, items, enum, default прямо в параметре) — поля уходят в schema;
//   - параметр in: body → requestBody application/json с его description и x-*; два тела или тело без schema —
//     ошибка, а не молчаливый выбор;
//   - ответ со schema без content (Swagger 2) → content application/json;
//   - anyOf из схемы и {type: null} (OpenAPI 3.1, compute) → схема с nullable: true; иначе oapi-codegen делает
//     анонимный union, и поле нельзя задать снаружи пакета. Остальные anyOf — настоящие объединения, не трогаются;
//   - ссылка на схему, которой нет в components.schemas (compute: VmResourceFloatingIp), — схема объявляется
//     объектом с любыми полями: в Go это map[string]any, данные не теряются.
//
// Что исправлено — в stderr, по строке на правило. Код выхода: 0 — записано, 1 — ошибка.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	flag.Parse()

	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: specfix in.yaml out.yaml")
		os.Exit(1)
	}

	if err := run(flag.Arg(0), flag.Arg(1)); err != nil {
		fmt.Fprintln(os.Stderr, "specfix:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	b, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}

	doc, log, err := fix(doc)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}

	for _, l := range log {
		fmt.Fprintf(os.Stderr, "specfix %s: %s\n", in, l)
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}

	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// fix правит документ на месте и возвращает его вместе с отчётом: правило и число правок.
func fix(doc map[string]any) (map[string]any, []string, error) {
	var log []string

	note := func(n int, what string) {
		if n > 0 {
			log = append(log, fmt.Sprintf("%s: %d", what, n))
		}
	}

	schemas := mapAt(mapAt(doc, "components"), "schemas")
	if defs, ok := doc["definitions"].(map[string]any); ok {
		for k, v := range defs {
			schemas[k] = v
		}

		delete(doc, "definitions")
		note(len(defs), "definitions → components.schemas")
	}

	for _, k := range []string{"swagger", "consumes", "produces"} {
		delete(doc, k)
	}

	note(rewriteRefs(doc), "ссылки #/definitions/ → #/components/schemas/")

	var params, bodies, responses int

	paths, _ := doc["paths"].(map[string]any)
	for path, item := range paths {
		ops, _ := item.(map[string]any)
		for method, op := range ops {
			o, ok := op.(map[string]any)
			if !ok {
				continue
			}

			n, err := bodyToRequestBody(o)
			if err != nil {
				return nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
			}

			bodies += n
			params += paramSchemas(o)
			responses += responseContent(o)
		}
	}

	note(params, "параметры без schema")
	note(bodies, "in: body → requestBody")
	note(responses, "schema ответа → content")
	note(nullableAnyOf(doc), "anyOf [X, null] → X, nullable")

	missing := danglingSchemas(doc, schemas)
	for _, name := range missing {
		schemas[name] = map[string]any{"type": "object", "additionalProperties": true}
	}

	if len(missing) > 0 {
		log = append(log, fmt.Sprintf("несуществующие схемы → объект с любыми полями: %v", missing))
	}

	return doc, log, nil
}

// mapAt возвращает m[key] как map, создавая её при отсутствии.
func mapAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}

	v := map[string]any{}
	m[key] = v

	return v
}

const defsPrefix, schemasPrefix = "#/definitions/", "#/components/schemas/"

func rewriteRefs(v any) int {
	n := 0
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok && k == "$ref" && len(s) > len(defsPrefix) && s[:len(defsPrefix)] == defsPrefix {
				t[k] = schemasPrefix + s[len(defsPrefix):]
				n++

				continue
			}

			n += rewriteRefs(x)
		}
	case []any:
		for _, x := range t {
			n += rewriteRefs(x)
		}
	}

	return n
}

func bodyToRequestBody(op map[string]any) (int, error) {
	ps, _ := op["parameters"].([]any)
	kept := ps[:0:0]

	var body map[string]any

	for _, p := range ps {
		m, ok := p.(map[string]any)
		if !ok || m["in"] != "body" {
			kept = append(kept, p)
			continue
		}

		if body != nil {
			return 0, errors.New("два параметра in: body")
		}

		if m["schema"] == nil {
			return 0, errors.New("in: body без schema")
		}

		required, _ := m["required"].(bool)
		body = map[string]any{
			"required": required,
			"content":  map[string]any{"application/json": map[string]any{"schema": m["schema"]}},
		}

		for k, v := range m {
			if k == "description" || strings.HasPrefix(k, "x-") {
				body[k] = v
			}
		}
	}

	if body == nil {
		return 0, nil
	}

	if op["requestBody"] != nil {
		return 0, errors.New("in: body рядом с requestBody")
	}

	op["requestBody"] = body
	op["parameters"] = kept

	return 1, nil
}

func responseContent(op map[string]any) int {
	rs, _ := op["responses"].(map[string]any)

	n := 0
	for _, r := range rs {
		m, ok := r.(map[string]any)
		if !ok || m["schema"] == nil || m["content"] != nil {
			continue
		}

		m["content"] = map[string]any{"application/json": map[string]any{"schema": m["schema"]}}
		delete(m, "schema")

		n++
	}

	return n
}

// nullableAnyOf заменяет anyOf [X, {type: null}] на X с nullable: true. Свои поля узла (description, default)
// важнее полей X.
func nullableAnyOf(v any) int {
	n := 0

	switch t := v.(type) {
	case map[string]any:
		if alts, ok := t["anyOf"].([]any); ok && len(alts) == 2 {
			for i, a := range alts {
				if m, ok := a.(map[string]any); ok && len(m) == 1 && hasKey(m, "type") && (m["type"] == nil || m["type"] == "null") {
					x, _ := alts[1-i].(map[string]any)

					delete(t, "anyOf")

					for k, xv := range x {
						if _, own := t[k]; !own {
							t[k] = xv
						}
					}

					t["nullable"] = true
					n++

					break
				}
			}
		}

		for _, x := range t {
			n += nullableAnyOf(x)
		}
	case []any:
		for _, x := range t {
			n += nullableAnyOf(x)
		}
	}

	return n
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]

	return ok
}

// schemaKeys — поля Swagger 2, которые в OpenAPI 3 живут внутри schema параметра.
var schemaKeys = []string{"type", "format", "items", "enum", "default"}

func paramSchemas(op map[string]any) int {
	ps, _ := op["parameters"].([]any)

	n := 0
	for _, p := range ps {
		m, ok := p.(map[string]any)
		if !ok || m["$ref"] != nil || m["schema"] != nil || m["content"] != nil {
			continue
		}

		s := map[string]any{}
		for _, k := range schemaKeys {
			if v, ok := m[k]; ok {
				s[k] = v
				delete(m, k)
			}
		}

		if len(s) == 0 {
			s["type"] = "string"
		}

		m["schema"] = s
		n++
	}

	return n
}

var schemaRef = regexp.MustCompile(`^#/components/schemas/(.+)$`)

func danglingSchemas(doc, schemas map[string]any) []string {
	seen := map[string]bool{}

	var walk func(v any)

	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if s, ok := t["$ref"].(string); ok {
				if m := schemaRef.FindStringSubmatch(s); m != nil && schemas[m[1]] == nil {
					seen[m[1]] = true
				}
			}

			for _, x := range t {
				walk(x)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(doc)

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}
