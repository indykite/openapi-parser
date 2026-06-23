// Copyright (c) 2026 IndyKite
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gen

import (
	"go/ast"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// resolver turns data-type tokens into Schema nodes. Resolution is now
// context-aware: a refCtx carries the file a reference appears in, so qualified
// names (model.Account) resolve through that file's imports and package.
type resolver struct {
	src     *source
	schemas map[string]*Schema
	seen    map[string]bool // recursion guard, keyed by display name
}

// refCtx is the resolution context: the file the type token was referenced from.
type refCtx struct{ file string }

// combinedPattern matches model composition: Base{field=Type,...}. Mirrors
// swag's own pattern (base allows path/brackets, body is the rest).
var combinedPattern = regexp.MustCompile(`^([\w\-./\[\]]+)\{(.*)\}$`)

func (r *resolver) schemaForToken(tok string, ctx refCtx) *Schema {
	tok = strings.TrimSpace(tok)
	switch {
	case tok == "":
		return nil
	case tok == "interface{}" || tok == "any":
		return &Schema{} // empty schema matches anything
	case strings.HasPrefix(tok, "[]"):
		return &Schema{Type: []string{"array"}, Items: r.schemaForToken(tok[2:], ctx)}
	case strings.HasPrefix(tok, "*"):
		return makeNullable(r.schemaForToken(tok[1:], ctx))
	case strings.HasPrefix(tok, "map["):
		return r.mapSchema(tok, ctx)
	case strings.Contains(tok, "{"):
		return r.composition(tok, ctx)
	}
	if t, format, ok := primitiveType(tok); ok {
		s := &Schema{Type: []string{t}}
		if format != "" {
			s.Format = format
		}
		return s
	}
	if def := r.lookup(tok, ctx); def != nil {
		display, _ := r.resolveStruct(def)
		return &Schema{Ref: "#/components/schemas/" + display}
	}
	// Unresolved (likely a cross-MODULE / third-party type — TODO(coverage):
	// resolve via the module cache). Degrade to a permissive object rather than
	// failing the whole build.
	return &Schema{Type: []string{"object"}}
}

func (r *resolver) schemaForResponse(kind, dataType string, ctx refCtx) *Schema {
	switch kind {
	case "array":
		return &Schema{Type: []string{"array"}, Items: r.schemaForToken(dataType, ctx)}
	case "object", "":
		return r.schemaForToken(dataType, ctx)
	default:
		if t, format, ok := primitiveType(kind); ok {
			s := &Schema{Type: []string{t}}
			if format != "" {
				s.Format = format
			}
			return s
		}
		return r.schemaForToken(dataType, ctx)
	}
}

// mapSchema handles map[K]V -> object with additionalProperties V.
func (r *resolver) mapSchema(tok string, ctx refCtx) *Schema {
	_, after, found := strings.Cut(tok, "]")
	if !found {
		return &Schema{Type: []string{"object"}}
	}
	val := strings.TrimSpace(after)
	if val == "interface{}" || val == "any" || val == "" {
		return &Schema{Type: []string{"object"}} // free-form object
	}
	return &Schema{Type: []string{"object"}, AdditionalProperties: r.schemaForToken(val, ctx)}
}

// composition implements Base{field=Type,...} by cloning the base struct's
// object schema and overriding/adding the named fields. Handles a leading []
// (e.g. []JSONResult{data=X}) and nested composition in field values.
func (r *resolver) composition(tok string, ctx refCtx) *Schema {
	arrayDepth := 0
	for strings.HasPrefix(tok, "[]") {
		arrayDepth++
		tok = tok[2:]
	}
	m := combinedPattern.FindStringSubmatch(tok)
	if m == nil {
		return r.schemaForToken(tok, ctx) // not actually composed
	}
	base, body := m[1], m[2]

	schema := r.objectSchemaFor(base, ctx)
	if schema.Properties == nil {
		schema.Properties = map[string]*Schema{}
	}
	for _, part := range splitTopLevel(body) {
		left, right, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key := strings.TrimSpace(left)
		valTok := strings.TrimSpace(right)
		schema.Properties[key] = r.schemaForToken(valTok, ctx)
	}

	out := schema
	for range arrayDepth {
		out = &Schema{Type: []string{"array"}, Items: out}
	}
	return out
}

// objectSchemaFor resolves a base type to a CLONED inline object schema, so
// composition overrides don't mutate the shared component schema.
func (r *resolver) objectSchemaFor(base string, ctx refCtx) *Schema {
	if def := r.lookup(base, ctx); def != nil {
		display, s := r.resolveStruct(def)
		_ = display
		return cloneObjectSchema(s)
	}
	return &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
}

// --- type lookup (the cross-package core) --------------------------------

// lookup resolves a (possibly qualified) type token to a struct definition,
// honoring the referring file's imports and package.
func (r *resolver) lookup(tok string, ctx refCtx) *structDef {
	tok = strings.TrimSpace(tok)
	qual, name := "", tok
	if dot := strings.LastIndexByte(tok, '.'); dot >= 0 {
		qual, name = tok[:dot], tok[dot+1:]
	}

	if qual == "" {
		// same package as the referring file
		if ip := r.src.pathByFile[ctx.file]; ip != "" {
			if def := r.src.structsByPath[ip][name]; def != nil {
				return def
			}
		}
		return r.src.flat[name] // last-resort fallback
	}

	// qualified: try explicit import alias first
	if ip := r.src.importsByFile[ctx.file][qual]; ip != "" {
		if def := r.src.structsByPath[ip][name]; def != nil {
			return def
		}
	}
	// then match by package name (covers the common no-alias case)
	return r.findByPkgName(qual, name)
}

// findByPkgName finds a struct in any indexed package whose package name equals
// qual. Ambiguous when two packages share a name — TODO(coverage): disambiguate
// via the referring file's actual imports.
func (r *resolver) findByPkgName(qual, name string) *structDef {
	for ip, types := range r.src.structsByPath {
		if r.src.pkgNameByPath[ip] != qual {
			continue
		}
		if def := types[name]; def != nil {
			return def
		}
	}
	return nil
}

// resolveStruct builds (once) the component schema for a struct, resolving its
// field types in the struct's OWN file context. Returns (displayName, schema).
func (r *resolver) resolveStruct(def *structDef) (string, *Schema) {
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	display := def.name
	if rn := renameFromDoc(def.doc); rn != "" {
		display = rn
	} else if def.pkgName != "" {
		display = def.pkgName + "." + def.name // swag-style pkg-qualified key
	}
	if existing, ok := r.schemas[display]; ok {
		return display, existing
	}
	if r.seen[display] {
		return display, &Schema{Ref: "#/components/schemas/" + display}
	}
	r.seen[display] = true

	schema := &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
	if d := descriptionFromDoc(def.doc); d != "" {
		schema.Description = d
	}
	ctx := refCtx{file: def.file}
	for _, field := range def.fields {
		r.addField(schema, field, ctx)
	}
	r.schemas[display] = schema
	return display, schema
}

func (r *resolver) addField(schema *Schema, field *ast.Field, ctx refCtx) {
	tag := ""
	if field.Tag != nil {
		tag = strings.Trim(field.Tag.Value, "`")
	}
	if hasTag(tag, "swaggerignore", "true") {
		return
	}

	jsonName, jsonOpts := jsonTag(tag)
	var goName string
	if len(field.Names) > 0 {
		goName = field.Names[0].Name
	}
	name := jsonName
	if name == "" {
		name = goName
	}
	if name == "-" || name == "" {
		return // embedded/anonymous promotion is TODO(coverage)
	}

	var fieldSchema *Schema
	if override := tagValue(tag, "swaggertype"); override != "" {
		fieldSchema = schemaFromSwaggertype(override)
	} else {
		fieldSchema = r.schemaForToken(exprToToken(field.Type), ctx)
	}
	if fieldSchema == nil {
		fieldSchema = &Schema{Type: []string{"object"}}
	}
	applyFieldTags(fieldSchema, tag, field)
	schema.Properties[name] = fieldSchema

	required := hasTag(tag, "validate", "required") || hasTag(tag, "binding", "required")
	if required && !slices.Contains(jsonOpts, "omitempty") {
		schema.Required = append(schema.Required, name)
	}
}

// --- helpers -------------------------------------------------------------

// splitTopLevel splits a composition body on commas not nested in {} or [].
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if start <= len(s) {
		if last := strings.TrimSpace(s[start:]); last != "" {
			out = append(out, last)
		}
	}
	return out
}

func cloneObjectSchema(s *Schema) *Schema {
	if s == nil {
		return &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
	}
	// values are not mutated, only replaced — sharing is safe
	props := make(map[string]*Schema, len(s.Properties))
	maps.Copy(props, s.Properties)
	req := append([]string(nil), s.Required...)
	return &Schema{
		Type:        append([]string(nil), s.Type...),
		Properties:  props,
		Required:    req,
		Description: s.Description,
	}
}

func primitiveType(tok string) (typ, format string, ok bool) {
	switch tok {
	case "string":
		return "string", "", true
	case "bool", "boolean":
		return "boolean", "", true
	case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32", "rune", "byte":
		return "integer", "int32", true
	case "int64", "uint64":
		return "integer", "int64", true
	case "float32":
		return "number", "float", true
	case "float64", "number":
		return "number", "double", true
	case "time.Time":
		return "string", "date-time", true
	case "file":
		return "string", "binary", true
	}
	return "", "", false
}

func exprToToken(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprToToken(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToToken(t.X)
	case *ast.ArrayType:
		return "[]" + exprToToken(t.Elt)
	case *ast.MapType:
		return "map[" + exprToToken(t.Key) + "]" + exprToToken(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "object"
	}
}

func makeNullable(s *Schema) *Schema {
	if s == nil {
		return &Schema{Type: []string{"null"}}
	}
	if s.Ref != "" {
		// TODO(coverage): emit anyOf:[{$ref},{type:null}] for nullable refs.
		return s
	}
	if slices.Contains(s.Type, "null") {
		return s
	}
	s.Type = append(s.Type, "null")
	return s
}

func jsonTag(tag string) (name string, opts []string) {
	v := tagValue(tag, "json")
	if v == "" {
		return "", nil
	}
	parts := strings.Split(v, ",")
	return parts[0], parts[1:]
}

func tagValue(tag, key string) string {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		j := 0
		for j < len(tag) && tag[j] != ':' && tag[j] != ' ' {
			j++
		}
		if j >= len(tag) || tag[j] != ':' {
			break
		}
		k := tag[:j]
		tag = tag[j+1:]
		if tag == "" || tag[0] != '"' {
			break
		}
		tag = tag[1:]
		end := strings.IndexByte(tag, '"')
		if end < 0 {
			break
		}
		val := tag[:end]
		tag = tag[end+1:]
		if k == key {
			return val
		}
	}
	return ""
}

func hasTag(tag, key, want string) bool {
	return slices.Contains(strings.Split(tagValue(tag, key), ","), want)
}

func applyFieldTags(s *Schema, tag string, field *ast.Field) {
	if ex := tagValue(tag, "example"); ex != "" {
		s.Example = ex
	}
	if en := tagValue(tag, "enums"); en != "" {
		for v := range strings.SplitSeq(en, ",") {
			s.Enum = append(s.Enum, strings.TrimSpace(v))
		}
	}
	if fmtTag := tagValue(tag, "format"); fmtTag != "" {
		s.Format = fmtTag
	}
	if field.Doc != nil {
		s.Description = strings.TrimSpace(field.Doc.Text())
	} else if field.Comment != nil {
		s.Description = strings.TrimSpace(field.Comment.Text())
	}
}

func schemaFromSwaggertype(spec string) *Schema {
	parts := strings.Split(spec, ",")
	switch parts[0] {
	case "array":
		inner := "string"
		if len(parts) > 1 {
			inner = parts[1]
		}
		t, f, _ := primitiveType(inner)
		return &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{t}, Format: f}}
	case "primitive":
		if len(parts) > 1 {
			t, f, _ := primitiveType(parts[1])
			return &Schema{Type: []string{t}, Format: f}
		}
	default:
		if t, f, ok := primitiveType(parts[0]); ok {
			return &Schema{Type: []string{t}, Format: f}
		}
	}
	return &Schema{Type: []string{"string"}}
}

func renameFromDoc(doc string) string {
	for line := range strings.SplitSeq(doc, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "@name "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func descriptionFromDoc(doc string) string {
	var lines []string
	for line := range strings.SplitSeq(doc, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "@Description "); ok {
			lines = append(lines, rest)
		}
	}
	return strings.Join(lines, " ")
}

func normalizeMime(alias string) string {
	switch alias {
	case "json":
		return "application/json"
	case "xml":
		return "text/xml"
	case "plain":
		return "text/plain"
	case "html":
		return "text/html"
	case "mpfd", "multipart/form-data":
		return "multipart/form-data"
	case "x-www-form-urlencoded":
		return "application/x-www-form-urlencoded"
	case "json-api":
		return "application/vnd.api+json"
	case "octet-stream":
		return "application/octet-stream"
	case "event-stream":
		return "text/event-stream"
	}
	return alias
}
