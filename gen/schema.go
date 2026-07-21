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
	"strconv"
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

// splitComposition detects model composition syntax `Base{field=Type,...}`:
// a base type token followed immediately by a braced field-override body.
func splitComposition(tok string) (base, body string, ok bool) {
	open := strings.IndexByte(tok, '{')
	if open <= 0 || !strings.HasSuffix(tok, "}") {
		return "", "", false
	}
	return tok[:open], tok[open+1 : len(tok)-1], true
}

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
	if base, args, ok := splitGeneric(tok); ok {
		if display, _ := r.instantiate(base, args, ctx); display != "" {
			return &Schema{Ref: "#/components/schemas/" + display}
		}
	}
	if def := r.lookup(tok, ctx); def != nil {
		if def.alias != nil {
			return r.aliasSchema(def)
		}
		display, _ := r.resolveStruct(def)
		return &Schema{Ref: "#/components/schemas/" + display}
	}
	// Unresolved (likely a cross-MODULE / third-party type - TODO(coverage):
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
		// open map: the empty AdditionalProperties schema permits any value
		// (emitted as additionalProperties: true)
		return &Schema{Type: []string{"object"}, AdditionalProperties: &Schema{}}
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
	base, body, ok := splitComposition(tok)
	if !ok {
		// malformed (e.g. unbalanced braces): degrade instead of recursing
		return &Schema{Type: []string{"object"}}
	}

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
	if b, args, ok := splitGeneric(base); ok {
		if _, s := r.instantiate(b, args, ctx); s != nil {
			return cloneObjectSchema(s)
		}
	}
	if def := r.lookup(base, ctx); def != nil {
		if def.alias != nil {
			return cloneObjectSchema(r.aliasSchema(def))
		}
		_, s := r.resolveStruct(def)
		return cloneObjectSchema(s)
	}
	return &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
}

// aliasSchema inlines a named non-struct type (type X []Y, type X string) by
// resolving its underlying type in the defining file's context. Self-recursive
// named types (type A []A) degrade to a permissive object.
func (r *resolver) aliasSchema(def *structDef) *Schema {
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	key := "alias:" + def.importPath + "." + def.name
	if r.seen[key] {
		return &Schema{Type: []string{"object"}}
	}
	r.seen[key] = true
	defer delete(r.seen, key)
	return r.schemaForToken(exprToToken(def.alias), refCtx{file: def.file})
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
// qual. Ambiguous when two packages share a name - TODO(coverage): disambiguate
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
	display := displayName(def)
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
		r.addField(schema, field, ctx, nil, ctx)
	}
	r.schemas[display] = schema
	return display, schema
}

// displayName is the component key for a struct: the @name override if given,
// else the swag-style pkg-qualified name.
func displayName(def *structDef) string {
	if rn := renameFromDoc(def.doc); rn != "" {
		return rn
	}
	if def.pkgName != "" {
		return def.pkgName + "." + def.name
	}
	return def.name
}

// addField adds one struct field to schema. subst maps generic type-parameter
// names to their argument tokens; tokens that get substituted resolve in
// argCtx (the file the instantiation was referenced from) instead of ctx.
func (r *resolver) addField(schema *Schema, field *ast.Field, ctx refCtx, subst map[string]string, argCtx refCtx) {
	tag := ""
	if field.Tag != nil {
		tag = strings.Trim(field.Tag.Value, "`")
	}
	if hasTag(tag, "swaggerignore", "true") {
		return
	}

	jsonName, _ := jsonTag(tag)
	var goName string
	if len(field.Names) > 0 {
		goName = field.Names[0].Name
	}
	if goName != "" && !ast.IsExported(goName) {
		return // unexported fields are never marshaled by encoding/json
	}
	name := jsonName
	if name == "" {
		name = goName
	}
	if name == "-" {
		return
	}
	if name == "" {
		// anonymous field with no json tag: promote like Go does
		r.promoteEmbedded(schema, field, ctx, subst, argCtx)
		return
	}

	var fieldSchema *Schema
	if override := tagValue(tag, "swaggertype"); override != "" {
		fieldSchema = schemaFromSwaggertype(override)
	} else {
		fieldSchema = r.schemaForToken(r.fieldToken(field, subst, &ctx, argCtx), ctx)
	}
	if fieldSchema == nil {
		fieldSchema = &Schema{Type: []string{"object"}}
	}
	applyFieldTags(fieldSchema, tag, field)
	schema.Properties[name] = fieldSchema

	// like swag, `required` wins even over json omitempty - a request field
	// can be mandatory while the response marshaler omits empty values
	if hasTag(tag, "validate", "required") || hasTag(tag, "binding", "required") {
		schema.Required = append(schema.Required, name)
	}
}

// fieldToken renders a field's type token, applying generic substitution.
// The resolution context switches to argCtx (where the instantiation was
// written) only when the token consists purely of type parameters and
// builtins — a mixed token like `Box[T]` keeps resolving Box in the
// generic's own file, which is where that name is meaningful.
func (*resolver) fieldToken(field *ast.Field, subst map[string]string, ctx *refCtx, argCtx refCtx) string {
	tok := exprToToken(field.Type)
	if len(subst) == 0 {
		return tok
	}
	sub, changed := substituteToken(tok, subst)
	if changed && pureParamToken(tok, subst) {
		*ctx = argCtx
	}
	return sub
}

// pureParamToken reports whether every identifier in a type token is a type
// parameter or a builtin — i.e. the substituted token carries no name that
// must resolve in the generic's own package.
func pureParamToken(tok string, subst map[string]string) bool {
	found := false
	for i := 0; i < len(tok); {
		if !isIdentChar(tok[i]) {
			i++
			continue
		}
		j := i
		for j < len(tok) && isIdentChar(tok[j]) {
			j++
		}
		word := tok[i:j]
		i = j
		if _, isParam := subst[word]; isParam {
			found = true
			continue
		}
		if word == "map" || word == "any" || word == "interface" {
			continue
		}
		if _, _, ok := primitiveType(word); ok {
			continue
		}
		return false // a real type name that belongs to the generic's package
	}
	return found
}

// promoteEmbedded flattens an anonymous field's object schema into the parent,
// mirroring Go's field promotion. Outer fields win: properties already present
// are kept, and outer fields processed later simply overwrite promoted ones.
func (r *resolver) promoteEmbedded(
	schema *Schema, field *ast.Field, ctx refCtx, subst map[string]string, argCtx refCtx,
) {
	tok := strings.TrimPrefix(r.fieldToken(field, subst, &ctx, argCtx), "*")
	emb := r.schemaForToken(tok, ctx)
	if emb != nil && emb.Ref != "" {
		emb = r.schemas[strings.TrimPrefix(emb.Ref, "#/components/schemas/")]
	}
	if emb == nil {
		return // unresolved or mid-recursion embed: nothing to promote
	}
	for name, prop := range emb.Properties {
		if _, exists := schema.Properties[name]; !exists {
			schema.Properties[name] = prop
		}
	}
	for _, req := range emb.Required {
		if !slices.Contains(schema.Required, req) {
			schema.Required = append(schema.Required, req)
		}
	}
}

// --- generics (swaggo bracket syntax) --------------------------------------

// splitGeneric detects generic instantiation syntax `Base[Arg1,Arg2]` (swaggo
// annotation form and Go source form alike). Leading `[]`/`map[` tokens are
// handled by schemaForToken before this is consulted.
func splitGeneric(tok string) (base string, args []string, ok bool) {
	if !strings.HasSuffix(tok, "]") {
		return "", nil, false
	}
	open := strings.IndexByte(tok, '[')
	if open <= 0 {
		return "", nil, false
	}
	args = splitTopLevel(tok[open+1 : len(tok)-1])
	if len(args) == 0 {
		return "", nil, false
	}
	return tok[:open], args, true
}

// instantiate builds (once) the component schema for a generic struct applied
// to concrete type arguments. Fields are resolved in the generic's own file
// context with type parameters substituted by the argument tokens, which
// themselves resolve in the referring context. Returns ("", nil) when base is
// not a known generic type.
func (r *resolver) instantiate(base string, args []string, ctx refCtx) (string, *Schema) {
	def := r.lookup(base, ctx)
	if def == nil || def.alias != nil || len(def.typeParams) == 0 {
		return "", nil
	}
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	display := r.instanceDisplay(def, args, ctx)
	if existing, ok := r.schemas[display]; ok {
		return display, existing
	}
	if r.seen[display] {
		return display, &Schema{Ref: "#/components/schemas/" + display}
	}
	r.seen[display] = true

	subst := map[string]string{}
	for i, p := range def.typeParams {
		if i < len(args) {
			subst[p] = args[i]
		}
	}
	schema := &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
	if d := descriptionFromDoc(def.doc); d != "" {
		schema.Description = d
	}
	for _, field := range def.fields {
		r.addField(schema, field, refCtx{file: def.file}, subst, ctx)
	}
	r.schemas[display] = schema
	return display, schema
}

// instanceDisplay names an instantiation swag-style: Base-Arg1-Arg2 (component
// keys may only contain [A-Za-z0-9.\-_], so no literal brackets).
func (r *resolver) instanceDisplay(def *structDef, args []string, ctx refCtx) string {
	parts := []string{displayName(def)}
	for _, a := range args {
		parts = append(parts, r.argDisplay(a, ctx))
	}
	return strings.Join(parts, "-")
}

// argDisplay renders one type argument for use inside a component key. The
// package-separator dot is folded to '_' inside the argument segment
// (Base-pkg_Type, swag's convention): component keys flow into the type names
// SDK generators produce, so matching swag keeps those names stable across
// the migration.
func (r *resolver) argDisplay(tok string, ctx refCtx) string {
	var prefix strings.Builder
	for strings.HasPrefix(tok, "[]") {
		prefix.WriteString("array_")
		tok = tok[2:]
	}
	tok = strings.TrimPrefix(tok, "*")
	name := sanitizeComponentKey(tok)
	if b, nested, ok := splitGeneric(tok); ok {
		if display, _ := r.instantiate(b, nested, ctx); display != "" {
			name = display
		}
	} else if def := r.lookup(tok, ctx); def != nil {
		name = displayName(def)
	}
	return prefix.String() + strings.ReplaceAll(name, ".", "_")
}

var componentKeyDisallowed = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func sanitizeComponentKey(s string) string {
	return componentKeyDisallowed.ReplaceAllString(s, "_")
}

// substituteToken replaces whole identifiers in a type token according to
// subst, skipping selector members (the `D` in `pkg.D` is not a type param).
func substituteToken(tok string, subst map[string]string) (out string, changed bool) {
	var b strings.Builder
	for i := 0; i < len(tok); {
		c := tok[i]
		if !isIdentChar(c) || (c >= '0' && c <= '9') {
			b.WriteByte(c)
			i++
			continue
		}
		j := i
		for j < len(tok) && isIdentChar(tok[j]) {
			j++
		}
		word := tok[i:j]
		if rep, ok := subst[word]; ok && (i == 0 || tok[i-1] != '.') {
			b.WriteString(rep)
			changed = true
		} else {
			b.WriteString(word)
		}
		i = j
	}
	return b.String(), changed
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
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
	// values are not mutated, only replaced - sharing is safe
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
	case "integer": // JSON type name, used by swaggertype tags
		return "integer", "", true
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
	case *ast.IndexExpr: // generic instantiation: List[T]
		return exprToToken(t.X) + "[" + exprToToken(t.Index) + "]"
	case *ast.IndexListExpr: // generic instantiation: Pair[K, V]
		args := make([]string, len(t.Indices))
		for i, idx := range t.Indices {
			args[i] = exprToToken(idx)
		}
		return exprToToken(t.X) + "[" + strings.Join(args, ",") + "]"
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
		// $ref allows no sibling type keyword; wrap instead.
		return &Schema{AnyOf: []*Schema{s, {Type: []string{"null"}}}}
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
		// Tag values are Go quoted strings - scan escape-aware so an
		// embedded \" (e.g. a JSON example) doesn't truncate the value
		// and corrupt every tag after it.
		end := 1
		for end < len(tag) && tag[end] != '"' {
			if tag[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(tag) {
			break
		}
		quoted := tag[:end+1]
		tag = tag[end+1:]
		if k == key {
			val, err := strconv.Unquote(quoted)
			if err != nil {
				return strings.Trim(quoted, `"`)
			}
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
		s.Example = coerceScalar(ex, s)
	}
	if def := tagValue(tag, "default"); def != "" {
		s.Default = coerceScalar(def, s)
	}
	if en := tagValue(tag, "enums"); en != "" {
		target := enumTarget(s)
		for v := range strings.SplitSeq(en, ",") {
			target.Enum = append(target.Enum, coerceScalar(strings.TrimSpace(v), target))
		}
	}
	if fmtTag := tagValue(tag, "format"); fmtTag != "" {
		s.Format = fmtTag
	}
	if v := tagValue(tag, "minimum"); v != "" {
		s.Minimum = parseFloatPtr(v)
	}
	if v := tagValue(tag, "maximum"); v != "" {
		s.Maximum = parseFloatPtr(v)
	}
	if v := tagValue(tag, "multipleOf"); v != "" {
		s.MultipleOf = parseFloatPtr(v)
	}
	if v := tagValue(tag, "minLength"); v != "" {
		s.MinLength = parseIntPtr(v)
	}
	if v := tagValue(tag, "maxLength"); v != "" {
		s.MaxLength = parseIntPtr(v)
	}
	applyExtensionsTag(s, tagValue(tag, "extensions"))
	applyValidationRules(s, tagValue(tag, "binding"))
	applyValidationRules(s, tagValue(tag, "validate"))
	if field.Doc != nil {
		s.Description = strings.TrimSpace(field.Doc.Text())
	} else if field.Comment != nil {
		s.Description = strings.TrimSpace(field.Comment.Text())
	}
}

// applyExtensionsTag parses the swag `extensions` struct tag:
// `extensions:"x-nullable,x-unit=kg,!x-omitempty"` - bare keys become true,
// `!`-prefixed keys become false, and the `x-` prefix is added if missing.
func applyExtensionsTag(s *Schema, spec string) {
	for e := range strings.SplitSeq(spec, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		var value any = true
		if rest, negated := strings.CutPrefix(e, "!"); negated {
			e, value = rest, false
		}
		key, val, hasVal := strings.Cut(e, "=")
		if hasVal {
			value = val
		}
		if !strings.HasPrefix(key, "x-") {
			key = "x-" + key
		}
		if s.Extensions == nil {
			s.Extensions = map[string]any{}
		}
		s.Extensions[key] = value
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
	case "event-stream", "sse":
		return "text/event-stream"
	case "jsonl":
		return "application/jsonl"
	case "ndjson":
		return "application/x-ndjson"
	case "json-seq":
		return "application/json-seq"
	case "geo-json-seq":
		return "application/geo+json-seq"
	}
	return alias
}
