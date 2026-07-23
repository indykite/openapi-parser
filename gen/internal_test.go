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

// In-package ("white-box") tests for the unexported helpers: table-driven
// coverage of branches the black-box tests in gen_test.go don't reach. The
// internal_test.go name is the conventional marker for this — it matches the
// stdlib practice and the testpackage linter's default exemption.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeMimeTable(t *testing.T) {
	cases := map[string]string{
		"json":                   "application/json",
		"xml":                    "text/xml",
		"plain":                  "text/plain",
		"html":                   "text/html",
		"mpfd":                   "multipart/form-data",
		"multipart/form-data":    "multipart/form-data",
		"x-www-form-urlencoded":  "application/x-www-form-urlencoded",
		"json-api":               "application/vnd.api+json",
		"octet-stream":           "application/octet-stream",
		"event-stream":           "text/event-stream",
		"sse":                    "text/event-stream",
		"jsonl":                  "application/jsonl",
		"ndjson":                 "application/x-ndjson",
		"json-seq":               "application/json-seq",
		"geo-json-seq":           "application/geo+json-seq",
		"application/customized": "application/customized", // passthrough
	}
	for in, want := range cases {
		if got := normalizeMime(in); got != want {
			t.Errorf("normalizeMime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOAuthFlowNameTable(t *testing.T) {
	cases := map[string]string{
		"application": "clientCredentials",
		"implicit":    "implicit",
		"password":    "password",
		"accesscode":  "authorizationCode",
		"device":      "deviceAuthorization",
		"custom":      "custom", // passthrough
	}
	for in, want := range cases {
		if got := oauthFlowName(in); got != want {
			t.Errorf("oauthFlowName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoerceScalarTable(t *testing.T) {
	intSchema := &Schema{Type: []string{"integer"}}
	cases := []struct {
		schema *Schema
		want   any
		in     string
	}{
		{intSchema, int64(5), "5"},
		{intSchema, "5x", "5x"}, // unparseable stays string
		{&Schema{Type: []string{"number"}}, 1.5, "1.5"},
		{&Schema{Type: []string{"boolean"}}, true, "true"},
		{&Schema{Type: []string{"boolean"}}, false, "0"},
		{&Schema{Type: []string{"string"}}, "5", "5"},
		{&Schema{Type: []string{"null", "integer"}}, int64(7), "7"}, // skips null
		{nil, "x", "x"},
	}
	for _, c := range cases {
		if got := coerceScalar(c.in, c.schema); got != c.want {
			t.Errorf("coerceScalar(%q, %v) = %#v, want %#v", c.in, c.schema, got, c.want)
		}
	}
}

func TestCoerceScalarArrayTable(t *testing.T) {
	strArr := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"string"}}}
	intArr := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"integer"}}}
	cases := []struct {
		schema *Schema
		want   any
		in     string
	}{
		{strArr, []any{"a", "b"}, "a,b"},
		{strArr, []any{"a"}, "a"},                                  // single item, no comma
		{intArr, []any{int64(1), int64(2)}, "1, 2"},                // items trimmed and coerced
		{intArr, []any{int64(1), "2x"}, "1,2x"},                    // unparseable item stays string
		{&Schema{Type: []string{"array"}}, []any{"a", "b"}, "a,b"}, // nil Items: items stay strings
	}
	for _, c := range cases {
		if got := coerceScalar(c.in, c.schema); !reflect.DeepEqual(got, c.want) {
			t.Errorf("coerceScalar(%q, %v) = %#v, want %#v", c.in, c.schema, got, c.want)
		}
	}
}

func TestParseSecurityReqTable(t *testing.T) {
	cases := []struct {
		want map[string][]string
		in   string
	}{
		{map[string][]string{"ApiKeyAuth": nil}, "ApiKeyAuth"},
		{map[string][]string{"OAuth2Application": {"write", "admin"}}, "OAuth2Application[write, admin]"},
		// && combines schemes into a single requirement (all required together)
		{map[string][]string{"ApiKeyAuth": nil, "BearerAuth": nil}, "ApiKeyAuth && BearerAuth"},
		{map[string][]string{"OAuth2Application": {"write"}, "ApiKeyAuth": nil}, "OAuth2Application[write] && ApiKeyAuth"},
		{map[string][]string{"A": nil, "B": nil}, "  A  &&  B  "}, // whitespace tolerated
		{map[string][]string{"A": nil}, "A && "},                  // empty part skipped
		{nil, "[write]"},                                          // malformed: missing scheme name
		{nil, ""},                                                 // no schemes: nil, never {} ("no auth")
		{nil, " && "},                                             // separators only: nil as well
	}
	for _, c := range cases {
		if got := parseSecurityReq(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseSecurityReq(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestApplyValidationRulesTable(t *testing.T) {
	str := &Schema{Type: []string{"string"}}
	applyValidationRules(str, "min=3,max=10,len=4")
	if *str.MinLength != 4 || *str.MaxLength != 4 { // len overrides min/max
		t.Errorf("string len rule: %+v", str)
	}

	str2 := &Schema{Type: []string{"string"}}
	applyValidationRules(str2, "min=3,max=10")
	if *str2.MinLength != 3 || *str2.MaxLength != 10 {
		t.Errorf("string min/max rules: %+v", str2)
	}

	arr := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"string"}}}
	applyValidationRules(arr, "min=1,max=5")
	if *arr.MinItems != 1 || *arr.MaxItems != 5 {
		t.Errorf("array min/max rules: %+v", arr)
	}

	num := &Schema{Type: []string{"number"}}
	applyValidationRules(num, "gt=0,lt=10")
	if *num.ExclusiveMinimum != 0 || *num.ExclusiveMaximum != 10 {
		t.Errorf("gt/lt rules: %+v", num)
	}

	// quoted oneof values, applied to array items
	arr2 := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"string"}}}
	applyValidationRules(arr2, "oneof='New York' Boston")
	if !reflect.DeepEqual(arr2.Items.Enum, []any{"New York", "Boston"}) {
		t.Errorf("quoted oneof on array: %v", arr2.Items.Enum)
	}

	// dive: remaining rules apply to elements; without items it must stop.
	dive := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"string"}}}
	applyValidationRules(dive, "max=10,dive,json,min=96,max=8192")
	if *dive.MaxItems != 10 || *dive.Items.MinLength != 96 || *dive.Items.MaxLength != 8192 {
		t.Errorf("dive rules: %+v items %+v", dive, dive.Items)
	}
	applyValidationRules(&Schema{Type: []string{"array"}}, "dive,min=1") // nil items: no panic

	// custom validators must be ignored without panicking
	ignored := &Schema{Type: []string{"string"}}
	applyValidationRules(ignored, "required,gid=PROJECT,node_type,omitempty")
	applyValidationRules(nil, "min=1")
}

func TestMalformedSecurityEmitsNoRequirement(t *testing.T) {
	// A nil/empty parse result must not be appended: a {} requirement would
	// mean "no auth required". Guards live in applySecurity and parseOperation.
	api := &API{SecuritySchemes: map[string]SecurityScheme{}, Extensions: map[string]any{}}
	parseGeneral(parseCommentGroup(`@title T
@securitydefinitions.apikey K
@in header
@name Authorization
@security [write]
@security K`), api)
	if len(api.Security) != 1 {
		t.Fatalf("document security must keep only the valid requirement: %#v", api.Security)
	}
	if _, ok := api.Security[0]["K"]; !ok || len(api.Security[0]) != 1 {
		t.Fatalf("document security requirement must reference exactly scheme K: %#v", api.Security[0])
	}

	res := &resolver{src: &source{}, schemas: map[string]*Schema{}}
	op := parseOperation(parseCommentGroup(`@Router /x [get]
@Summary X
@Security [write]
@Security K`), res, refCtx{})
	if len(op.Security) != 1 {
		t.Fatalf("operation security must keep only the valid requirement: %#v", op.Security)
	}
}

func TestSchemeContextEndsAtNonSecurityDirective(t *testing.T) {
	api := &API{SecuritySchemes: map[string]SecurityScheme{}, Extensions: map[string]any{}}
	parseGeneral(parseCommentGroup(`@title T
@securitydefinitions.apikey K
@in header
@name Authorization
@host api.example.com
@description Public API for accounts.`), api)
	if api.Info.Description != "Public API for accounts." {
		t.Errorf("description after a non-security directive belongs to info: %q", api.Info.Description)
	}
	if api.SecuritySchemes["K"].Description != "" {
		t.Errorf("scheme must not absorb the info description: %q", api.SecuritySchemes["K"].Description)
	}
}

func TestSchemeContextEndsAtDocumentSecurity(t *testing.T) {
	// document-level @security is not a scheme attribute: it must end the
	// scheme section, so a following @description belongs to info.
	api := &API{SecuritySchemes: map[string]SecurityScheme{}, Extensions: map[string]any{}}
	parseGeneral(parseCommentGroup(`@title T
@securitydefinitions.apikey K
@in header
@name Authorization
@security K
@description Public API for accounts.`), api)
	if api.Info.Description != "Public API for accounts." {
		t.Errorf("description after @security belongs to info: %q", api.Info.Description)
	}
	if api.SecuritySchemes["K"].Description != "" {
		t.Errorf("scheme must not absorb the description: %q", api.SecuritySchemes["K"].Description)
	}
	if len(api.Security) != 1 {
		t.Errorf("document security requirement lost: %v", api.Security)
	}
}

func TestPureParamToken(t *testing.T) {
	subst := map[string]string{"D": "X"}
	cases := map[string]bool{
		"D":               true,
		"[]D":             true,
		"*D":              true,
		"map[string]D":    true,
		"Box[D]":          false, // Box must resolve in the generic's package
		"pkg.D":           false, // selector member, not a param
		"map[string]Item": false,
	}
	for tok, want := range cases {
		if got := pureParamToken(tok, subst); got != want {
			t.Errorf("pureParamToken(%q) = %v, want %v", tok, got, want)
		}
	}
}

func TestMergeAttrTokens(t *testing.T) {
	got := mergeAttrTokens([]string{"Status", "Enums(active,", "inactive)", "minimum(1)"})
	want := []string{"Status", "Enums(active, inactive)", "minimum(1)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge: %v, want %v", got, want)
	}
	// unclosed attribute stays split; quoted text is never merged
	got = mergeAttrTokens([]string{"Enums(a,", "b", "c"})
	if !reflect.DeepEqual(got, []string{"Enums(a,", "b", "c"}) {
		t.Errorf("unclosed: %v", got)
	}
}

func TestFlatMapPrefersStructs(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	src := newSource()
	// alias indexed first must not shadow a same-named struct indexed later
	collectTypes("a.go", "m/a", "a", parse("package a\ntype Status string"), src)
	collectTypes("b.go", "m/b", "b", parse("package b\ntype Status struct{ X int }"), src)
	if def := src.flat["Status"]; def == nil || def.alias != nil {
		t.Errorf("struct should win the flat fallback, got %+v", def)
	}
}

func TestTagValueEscapes(t *testing.T) {
	tag := `json:"config" example:"{\"key\":\"value\"}" binding:"required,min=1"`
	if got := tagValue(tag, "example"); got != `{"key":"value"}` {
		t.Errorf("escaped value: %q", got)
	}
	if got := tagValue(tag, "binding"); got != "required,min=1" {
		t.Errorf("tag after escaped value: %q", got)
	}
	if got := tagValue(tag, "json"); got != "config" {
		t.Errorf("plain value: %q", got)
	}
}

func TestParseAttributesIdentifierKeysOnly(t *testing.T) {
	lead, attrs := parseAttributes(fields(`page query int false "page number (required when using cursor)" minimum(1)`))
	if attrs["minimum"] != "1" || len(attrs) != 1 {
		t.Errorf("attrs: %v", attrs)
	}
	joined := strings.Join(lead, " ")
	if !strings.Contains(joined, "(required when using cursor)") {
		t.Errorf("parenthesized description must stay in lead tokens: %v", lead)
	}
}

func TestApplyAttrsTable(t *testing.T) {
	s := &Schema{Type: []string{"integer"}}
	applyAttrs(s, map[string]string{
		"pattern":    `^\d+$`,
		"multipleof": "5",
		"example":    "10",
		"format":     "int64",
	})
	if s.Pattern != `^\d+$` || *s.MultipleOf != 5 || s.Example != int64(10) || s.Format != "int64" {
		t.Errorf("attrs not applied: %+v", s)
	}

	arr := &Schema{Type: []string{"array"}, Items: &Schema{Type: []string{"integer"}}}
	applyAttrs(arr, map[string]string{"minitems": "1", "maxitems": "3", "enums": "1,2"})
	if *arr.MinItems != 1 || *arr.MaxItems != 3 {
		t.Errorf("array item bounds: %+v", arr)
	}
	if !reflect.DeepEqual(arr.Items.Enum, []any{int64(1), int64(2)}) { // enums target items
		t.Errorf("array enums should coerce onto items: %v", arr.Items.Enum)
	}
}

func TestResponseDescriptionTable(t *testing.T) {
	cases := []struct {
		r    Response
		want string
	}{
		{Response{Description: "custom"}, "custom"},
		{Response{Code: "404"}, "Not Found"},
		{Response{Code: "default"}, ""},
	}
	for _, c := range cases {
		if got := responseDescription(&c.r); got != c.want {
			t.Errorf("responseDescription(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestMakeNullableTable(t *testing.T) {
	if s := makeNullable(nil); !reflect.DeepEqual(s.Type, []string{"null"}) {
		t.Errorf("nil: %+v", s)
	}
	ref := makeNullable(&Schema{Ref: "#/components/schemas/X"})
	if len(ref.AnyOf) != 2 || ref.AnyOf[0].Ref == "" || !reflect.DeepEqual(ref.AnyOf[1].Type, []string{"null"}) {
		t.Errorf("ref: %+v", ref)
	}
	prim := makeNullable(&Schema{Type: []string{"string"}})
	if !reflect.DeepEqual(prim.Type, []string{"string", "null"}) {
		t.Errorf("primitive: %+v", prim)
	}
	if again := makeNullable(prim); len(again.Type) != 2 { // idempotent
		t.Errorf("idempotency: %+v", again)
	}
}

func TestStripNullableTable(t *testing.T) {
	if stripNullable(nil) != nil {
		t.Error("nil should stay nil")
	}
	prim := stripNullable(&Schema{Type: []string{"string", "null"}})
	if !reflect.DeepEqual(prim.Type, []string{"string"}) {
		t.Errorf("primitive: %+v", prim)
	}
	bare := stripNullable(makeNullable(&Schema{Ref: "#/components/schemas/X"}))
	if bare.Ref != "#/components/schemas/X" || len(bare.AnyOf) != 0 {
		t.Errorf("bare wrapper should collapse to the inner ref: %+v", bare)
	}
	rich := makeNullable(&Schema{Ref: "#/components/schemas/X"})
	rich.Description = "doc"
	got := stripNullable(rich)
	if got.Description != "doc" || len(got.AnyOf) != 1 || got.AnyOf[0].Ref == "" {
		t.Errorf("wrapper metadata should survive minus the null branch: %+v", got)
	}
}

func TestArrayItemsMultiPointerNotNullable(t *testing.T) {
	for _, tok := range []string{"[]*int", "[]**int", "[]***int"} {
		s := (&resolver{}).schemaForToken(tok, refCtx{})
		if s.Items == nil || !reflect.DeepEqual(s.Items.Type, []string{"integer"}) {
			t.Errorf("%s items should be plain integer: %+v", tok, s.Items)
		}
	}
}

func TestExprToTokenTable(t *testing.T) {
	d := ast.NewIdent("D")
	cases := []struct {
		expr ast.Expr
		want string
	}{
		{d, "D"},
		{&ast.StarExpr{X: d}, "*D"},
		{&ast.ArrayType{Elt: d}, "[]D"},
		{&ast.MapType{Key: ast.NewIdent("string"), Value: d}, "map[string]D"},
		{&ast.SelectorExpr{X: ast.NewIdent("pkg"), Sel: d}, "pkg.D"},
		{&ast.IndexExpr{X: ast.NewIdent("List"), Index: d}, "List[D]"},
		{&ast.IndexListExpr{X: ast.NewIdent("Pair"), Indices: []ast.Expr{ast.NewIdent("K"), d}}, "Pair[K,D]"},
		{&ast.InterfaceType{Methods: &ast.FieldList{}}, "interface{}"},
		{&ast.StructType{Fields: &ast.FieldList{}}, "object"}, // fallback
	}
	for _, c := range cases {
		if got := exprToToken(c.expr); got != c.want {
			t.Errorf("exprToToken(%T) = %q, want %q", c.expr, got, c.want)
		}
	}
}

func TestSubstituteTokenTable(t *testing.T) {
	subst := map[string]string{"D": "Account"}
	cases := []struct {
		in      string
		want    string
		changed bool
	}{
		{"D", "Account", true},
		{"[]D", "[]Account", true},
		{"*D", "*Account", true},
		{"pkg.D", "pkg.D", false}, // selector member is not a type param
		{"DD", "DD", false},       // whole identifiers only
		{"map[string]D", "map[string]Account", true},
	}
	for _, c := range cases {
		got, changed := substituteToken(c.in, subst)
		if got != c.want || changed != c.changed {
			t.Errorf("substituteToken(%q) = (%q, %v), want (%q, %v)", c.in, got, changed, c.want, c.changed)
		}
	}
}

func TestSplitGenericAndComposition(t *testing.T) {
	if base, args, ok := splitGeneric("List[T]"); !ok || base != "List" || !reflect.DeepEqual(args, []string{"T"}) {
		t.Errorf("List[T]: %q %v %v", base, args, ok)
	}
	if _, _, ok := splitGeneric("[]X"); ok {
		t.Error("[]X is not a generic instantiation")
	}
	if _, _, ok := splitGeneric("map[string]int"); ok {
		t.Error("map tokens are not generic instantiations")
	}
	if _, _, ok := splitGeneric("Plain"); ok {
		t.Error("Plain is not generic")
	}

	if base, body, ok := splitComposition("Base{a=b}"); !ok || base != "Base" || body != "a=b" {
		t.Errorf("Base{a=b}: %q %q %v", base, body, ok)
	}
	if _, _, ok := splitComposition("{orphan"); ok {
		t.Error("leading brace is not a composition")
	}
	if _, _, ok := splitComposition("Base{unclosed"); ok {
		t.Error("unclosed brace is not a composition")
	}
}

func TestParseRouterAndTruthy(t *testing.T) {
	if p, m := parseRouter("/x/{id} [POST]"); p != "/x/{id}" || m != "post" {
		t.Errorf("router: %q %q", p, m)
	}
	if p, m := parseRouter("/bare"); p != "/bare" || m != "get" {
		t.Errorf("bare router should default to get: %q %q", p, m)
	}
	truthy := map[string]bool{"true": true, "1": true, "yes": true, "required": true, "false": false, "": false}
	for in, want := range truthy {
		if isTruthy(in) != want {
			t.Errorf("isTruthy(%q) != %v", in, want)
		}
	}
}

func TestSplitOneOf(t *testing.T) {
	got := splitOneOf("'New York' Boston 'San Francisco'")
	want := []string{"New York", "Boston", "San Francisco"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitOneOf: %v, want %v", got, want)
	}
}

func TestYAMLStringTable(t *testing.T) {
	cases := map[string]string{
		"hello":            "hello",
		"application/json": "application/json",
		"/accounts/{id}":   `"/accounts/{id}"`,
		"Example API":      `"Example API"`,
		"true":             `"true"`,
		"No":               `"No"`,
		"on":               `"on"`,
		"y":                `"y"`,
		"3.2.0":            `"3.2.0"`,
		"200":              `"200"`,
		"":                 `""`,
		"$ref":             `"$ref"`,
		"line1\nline2":     `"line1\nline2"`,
	}
	for in, want := range cases {
		if got := yamlString(in); got != want {
			t.Errorf("yamlString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestYAMLScalarAndEmpties(t *testing.T) {
	if yamlScalar(nil) != "null" || yamlScalar(true) != "true" || yamlScalar(42) != "42" || yamlScalar(1.5) != "1.5" {
		t.Errorf("scalars: %s %s %s %s", yamlScalar(nil), yamlScalar(true), yamlScalar(42), yamlScalar(1.5))
	}
	var b strings.Builder
	writeYAMLValue(&b, map[string]any{}, 1)
	writeYAMLValue(&b, []any{}, 1)
	if b.String() != " {}\n []\n" {
		t.Errorf("empty collections: %q", b.String())
	}
}

func TestEmitJSONRoundTrip(t *testing.T) {
	api := &API{
		Schemas: map[string]*Schema{},
		Info:    Info{Title: "T", Version: "1"},
	}
	data, err := api.EmitJSON(EmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"openapi": "3.2.0"`) {
		t.Errorf("default version missing: %s", data)
	}
}

func TestPrimitiveTypeTable(t *testing.T) {
	cases := []struct {
		in, typ, format string
		ok              bool
	}{
		{"string", "string", "", true},
		{"bool", "boolean", "", true},
		{"boolean", "boolean", "", true},
		{"int", "integer", "int32", true},
		{"int8", "integer", "int32", true},
		{"uint16", "integer", "int32", true},
		{"rune", "integer", "int32", true},
		{"byte", "integer", "int32", true},
		{"int64", "integer", "int64", true},
		{"uint64", "integer", "int64", true},
		{"float32", "number", "float", true},
		{"float64", "number", "double", true},
		{"number", "number", "double", true},
		{"time.Time", "string", "date-time", true},
		{"file", "string", "binary", true},
		{"Account", "", "", false},
	}
	for _, c := range cases {
		typ, format, ok := primitiveType(c.in)
		if typ != c.typ || format != c.format || ok != c.ok {
			t.Errorf("primitiveType(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, typ, format, ok, c.typ, c.format, c.ok)
		}
	}
}

func TestSchemaFromSwaggertypeTable(t *testing.T) {
	arr := schemaFromSwaggertype("array,int64")
	if !reflect.DeepEqual(arr.Type, []string{"array"}) || arr.Items.Format != "int64" {
		t.Errorf("array,int64: %+v items %+v", arr, arr.Items)
	}
	arrDefault := schemaFromSwaggertype("array")
	if arrDefault.Items == nil || !reflect.DeepEqual(arrDefault.Items.Type, []string{"string"}) {
		t.Errorf("bare array should default to string items: %+v", arrDefault.Items)
	}
	prim := schemaFromSwaggertype("primitive,number")
	if !reflect.DeepEqual(prim.Type, []string{"number"}) {
		t.Errorf("primitive,number: %+v", prim)
	}
	// "integer" is the JSON type name; it must not degrade to string.
	bare := schemaFromSwaggertype("integer")
	if !reflect.DeepEqual(bare.Type, []string{"integer"}) || bare.Format != "" {
		t.Errorf("bare primitive: %+v", bare)
	}
	unknown := schemaFromSwaggertype("mystery")
	if !reflect.DeepEqual(unknown.Type, []string{"string"}) {
		t.Errorf("unknown swaggertype should degrade to string: %+v", unknown)
	}
}

func TestSanitizeComponentKey(t *testing.T) {
	if got := sanitizeComponentKey("map[string]int"); got != "map_string_int" {
		t.Errorf("sanitize: %q", got)
	}
	if got := sanitizeComponentKey("pkg.Type-A_b"); got != "pkg.Type-A_b" {
		t.Errorf("allowed chars must pass through: %q", got)
	}
}

func TestParseAttributesAndFields(t *testing.T) {
	lead, attrs := parseAttributes(fields(`id path int true "Account ID" minimum(1) enums(a,b)`))
	if !reflect.DeepEqual(lead, []string{"id", "path", "int", "true", "Account ID"}) {
		t.Errorf("lead: %v", lead)
	}
	if attrs["minimum"] != "1" || attrs["enums"] != "a,b" {
		t.Errorf("attrs: %v", attrs)
	}
}
