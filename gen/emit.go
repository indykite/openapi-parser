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

// Package gen parses swag-style Go annotations into an internal API model and
// emits OpenAPI 3.1/3.2 documents from it.
package gen

import (
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"strings"
)

// EmitOptions controls output.
type EmitOptions struct {
	// Version is the openapi version string to stamp. "3.2.0" (default) emits
	// 3.2 with its native constructs; "3.1.0" emits the backward-compatible
	// subset (no query/additionalOperations/itemSchema/hierarchical tags —
	// those are downgraded or dropped).
	Version string
}

// Emit renders the API model to an OpenAPI document tree.
func (api *API) Emit(opt EmitOptions) map[string]any {
	if opt.Version == "" {
		opt.Version = "3.2.0"
	}
	is32 := strings.HasPrefix(opt.Version, "3.2")

	doc := map[string]any{
		"openapi": opt.Version,
		"info":    emitInfo(&api.Info),
	}
	maps.Copy(doc, api.Extensions)
	if is32 && api.Self != "" {
		doc["$self"] = api.Self
	}
	if len(api.Servers) > 0 {
		doc["servers"] = emitServers(api.Servers, api.Schemes, is32)
	}
	if len(api.Security) > 0 {
		doc["security"] = emitSecurityReqs(api.Security)
	}
	if len(api.Tags) > 0 {
		doc["tags"] = emitTags(api.Tags, is32)
	}
	if api.ExternalDocs != nil {
		doc["externalDocs"] = emitExtDocs(api.ExternalDocs)
	}

	var pathOps, hookOps []Operation
	for i := range api.Operations {
		if api.Operations[i].Webhook != "" {
			hookOps = append(hookOps, api.Operations[i])
		} else {
			pathOps = append(pathOps, api.Operations[i])
		}
	}
	doc["paths"] = emitPaths(pathOps, api.Produces(), is32)
	if len(hookOps) > 0 {
		doc["webhooks"] = emitWebhooks(hookOps, api.Produces(), is32)
	}

	components := map[string]any{}
	if len(api.Schemas) > 0 {
		components["schemas"] = emitSchemas(api.Schemas)
	}
	if len(api.SecuritySchemes) > 0 {
		components["securitySchemes"] = emitSecuritySchemes(api.SecuritySchemes, is32)
	}
	if len(components) > 0 {
		doc["components"] = components
	}
	return doc
}

// Produces returns a default response content type.
func (*API) Produces() string { return "application/json" }

// EmitJSON renders to indented JSON bytes.
func (api *API) EmitJSON(opt EmitOptions) ([]byte, error) {
	return json.MarshalIndent(api.Emit(opt), "", "  ")
}

func emitInfo(in *Info) map[string]any {
	m := map[string]any{"title": in.Title, "version": in.Version}
	put(m, "summary", in.Summary)
	if in.Description != "" {
		m["description"] = in.Description
	}
	if in.TermsOfService != "" {
		m["termsOfService"] = in.TermsOfService
	}
	if c := in.Contact; c != (Contact{}) {
		cm := map[string]any{}
		put(cm, "name", c.Name)
		put(cm, "url", c.URL)
		put(cm, "email", c.Email)
		m["contact"] = cm
	}
	if l := in.License; l != (License{}) {
		lm := map[string]any{}
		put(lm, "name", l.Name)
		put(lm, "identifier", l.Identifier)
		put(lm, "url", l.URL)
		m["license"] = lm
	}
	return m
}

func emitServers(servers []Server, schemes []string, is32 bool) []any {
	if len(schemes) == 0 {
		schemes = []string{"https"}
	}
	out := make([]any, 0, len(servers))
	for i := range servers {
		s := &servers[i]
		urls := []string{s.URL}
		// foldServer leaves host-derived URLs protocol-relative (// prefix);
		// expand those into one server per @schemes entry (default https).
		if strings.HasPrefix(s.URL, "//") {
			urls = urls[:0]
			for _, scheme := range schemes {
				urls = append(urls, scheme+":"+s.URL)
			}
		}
		for _, url := range urls {
			sm := map[string]any{"url": url}
			put(sm, "description", s.Description)
			if is32 && len(urls) == 1 {
				// name must be unique, so never stamp it on scheme-expanded copies
				put(sm, "name", s.Name)
			}
			if len(s.Variables) > 0 {
				sm["variables"] = emitServerVariables(s.Variables)
			}
			out = append(out, sm)
		}
	}
	return out
}

func emitServerVariables(vars map[string]ServerVariable) map[string]any {
	out := map[string]any{}
	for name, v := range vars {
		vm := map[string]any{"default": v.Default}
		if len(v.Enum) > 0 {
			vm["enum"] = toAnySlice(v.Enum)
		}
		put(vm, "description", v.Description)
		out[name] = vm
	}
	return out
}

func emitTags(tags []Tag, is32 bool) []any {
	out := make([]any, 0, len(tags))
	for _, t := range tags {
		tm := map[string]any{"name": t.Name}
		put(tm, "description", t.Description)
		if is32 {
			// native 3.2 hierarchical fields
			put(tm, "summary", t.Summary)
			put(tm, "parent", t.Parent)
			put(tm, "kind", t.Kind)
		}
		if t.ExternalDocs != nil {
			tm["externalDocs"] = emitExtDocs(t.ExternalDocs)
		}
		out = append(out, tm)
	}
	return out
}

func emitExtDocs(e *ExternalDocs) map[string]any {
	m := map[string]any{"url": e.URL}
	put(m, "description", e.Description)
	return m
}

func emitPaths(ops []Operation, produce string, is32 bool) map[string]any {
	paths := map[string]any{}
	for i := range ops {
		op := &ops[i]
		item, ok := paths[op.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[op.Path] = item
		}
		placeOperation(item, op, emitOperation(op, produce, is32), is32)
	}
	return paths
}

// emitWebhooks groups webhook operations into the top-level webhooks map:
// name -> path item (3.1+).
func emitWebhooks(ops []Operation, produce string, is32 bool) map[string]any {
	hooks := map[string]any{}
	for i := range ops {
		op := &ops[i]
		item, ok := hooks[op.Webhook].(map[string]any)
		if !ok {
			item = map[string]any{}
			hooks[op.Webhook] = item
		}
		placeOperation(item, op, emitOperation(op, produce, is32), is32)
	}
	return hooks
}

// placeOperation slots an operation object into a path item under the right
// method key, honoring 3.2's first-class query and additionalOperations.
func placeOperation(item map[string]any, op *Operation, opObj map[string]any, is32 bool) {
	switch {
	case op.Method == "query" && is32:
		item["query"] = opObj // 3.2 first-class
	case isStandardMethod(op.Method):
		item[op.Method] = opObj
	case is32:
		// non-standard verb => additionalOperations (3.2)
		addl, ok := item["additionalOperations"].(map[string]any)
		if !ok {
			addl = map[string]any{}
			item["additionalOperations"] = addl
		}
		addl[strings.ToUpper(op.Method)] = opObj
	default:
		// 3.1 downgrade: best effort, treat query as post
		item["post"] = opObj
	}
}

func emitOperation(op *Operation, produce string, is32 bool) map[string]any {
	m := map[string]any{}
	put(m, "summary", op.Summary)
	put(m, "description", op.Description)
	put(m, "operationId", op.ID)
	if len(op.Tags) > 0 {
		m["tags"] = toAnySlice(op.Tags)
	}
	if op.Deprecated {
		m["deprecated"] = true
	}
	if op.ExternalDocs != nil {
		m["externalDocs"] = emitExtDocs(op.ExternalDocs)
	}
	if len(op.Params) > 0 {
		var params []any
		for i := range op.Params {
			params = append(params, emitParam(&op.Params[i]))
		}
		m["parameters"] = params
	}
	switch {
	case op.Body != nil:
		cts := op.Consumes
		if len(cts) == 0 {
			cts = []string{"application/json"}
		}
		content := map[string]any{}
		for _, ct := range cts {
			content[ct] = map[string]any{"schema": emitSchema(op.Body.Schema)}
		}
		body := map[string]any{
			"required": op.Body.Required,
			"content":  content,
		}
		put(body, "description", op.Body.Description)
		m["requestBody"] = body
	case len(op.Form) > 0:
		m["requestBody"] = emitFormBody(op)
	}
	m["responses"] = emitResponses(op, produce, is32)
	if len(op.Security) > 0 {
		m["security"] = emitSecurityReqs(op.Security)
	}
	maps.Copy(m, op.Extensions)
	return m
}

// emitFormBody assembles formData params into one form request body: an
// object schema with a property per field, under the operation's declared
// form media type (multipart/form-data unless @Accept says urlencoded).
func emitFormBody(op *Operation) map[string]any {
	ct := "multipart/form-data"
	for _, c := range op.Consumes {
		if c == "application/x-www-form-urlencoded" || c == "multipart/form-data" {
			ct = c
			break
		}
	}
	schema := &Schema{Type: []string{"object"}, Properties: map[string]*Schema{}}
	required := false
	for i := range op.Form {
		p := &op.Form[i]
		fs := p.Schema
		if fs == nil {
			fs = &Schema{Type: []string{"string"}}
		}
		if p.Description != "" && fs.Description == "" {
			fs.Description = p.Description
		}
		schema.Properties[p.Name] = fs
		if p.Required {
			schema.Required = append(schema.Required, p.Name)
			required = true
		}
	}
	return map[string]any{
		"required": required,
		"content":  map[string]any{ct: map[string]any{"schema": emitSchema(schema)}},
	}
}

func emitParam(p *Param) map[string]any {
	m := map[string]any{
		"name":     p.Name,
		"in":       p.In,
		"required": p.Required,
	}
	if p.Description != "" {
		m["description"] = p.Description
	}
	// parameter-level attributes (the rest apply to the schema below)
	if v, ok := p.Attributes["deprecated"]; ok && (v == "" || isTruthy(v)) {
		m["deprecated"] = true
	}
	if v, ok := p.Attributes["style"]; ok {
		m["style"] = v
	}
	if v, ok := p.Attributes["explode"]; ok {
		m["explode"] = isTruthy(v)
	}
	if v, ok := p.Attributes["allowreserved"]; ok && (v == "" || isTruthy(v)) {
		m["allowReserved"] = true
	}
	// schema-shaped attributes were applied at parse time (emit is pure)
	schema := p.Schema
	if schema == nil {
		schema = &Schema{Type: []string{"string"}}
	}
	m["schema"] = emitSchema(schema)
	return m
}

func emitResponses(op *Operation, produce string, is32 bool) map[string]any {
	out := map[string]any{}
	produces := op.Produces
	if len(produces) == 0 {
		produces = []string{produce}
	}
	for _, r := range op.Responses {
		respObj := map[string]any{"description": responseDescription(&r)}
		if is32 {
			put(respObj, "summary", r.Summary)
		}
		if r.Schema != nil {
			content := map[string]any{}
			for _, mt := range produces {
				schemaKey := "schema"
				if is32 && isSequentialMedia(mt) {
					// 3.2 streaming: the schema describes each event/item
					schemaKey = "itemSchema"
				}
				content[mt] = map[string]any{schemaKey: emitSchema(r.Schema)}
			}
			respObj["content"] = content
		}
		if len(r.Headers) > 0 {
			hs := map[string]any{}
			for name, h := range r.Headers {
				hm := map[string]any{"schema": map[string]any{"type": h.Type}}
				put(hm, "description", h.Description)
				hs[name] = hm
			}
			respObj["headers"] = hs
		}
		out[r.Code] = respObj
	}
	if len(out) == 0 {
		out["200"] = map[string]any{"description": "OK"}
	}
	return out
}

func emitSchemas(schemas map[string]*Schema) map[string]any {
	out := map[string]any{}
	for name, s := range schemas {
		out[name] = emitSchema(s)
	}
	return out
}

func emitSchema(s *Schema) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	if s.Ref != "" {
		return map[string]any{"$ref": s.Ref}
	}
	m := map[string]any{}
	if len(s.AnyOf) > 0 {
		anyOf := make([]any, len(s.AnyOf))
		for i, sub := range s.AnyOf {
			anyOf[i] = emitSchema(sub)
		}
		m["anyOf"] = anyOf
	}
	if len(s.Type) == 1 {
		m["type"] = s.Type[0]
	} else if len(s.Type) > 1 {
		m["type"] = toAnySlice(s.Type) // 3.1/3.2 type array
	}
	put(m, "format", s.Format)
	put(m, "pattern", s.Pattern)
	put(m, "description", s.Description)
	if s.Example != nil {
		// JSON Schema 2020-12 (the 3.1/3.2 schema dialect) only defines the
		// plural `examples` keyword; singular `example` is ignored there.
		m["examples"] = []any{s.Example}
	}
	if s.Default != nil {
		m["default"] = s.Default
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	putFloat(m, "minimum", s.Minimum)
	putFloat(m, "maximum", s.Maximum)
	putFloat(m, "exclusiveMinimum", s.ExclusiveMinimum)
	putFloat(m, "exclusiveMaximum", s.ExclusiveMaximum)
	putFloat(m, "multipleOf", s.MultipleOf)
	putInt(m, "minLength", s.MinLength)
	putInt(m, "maxLength", s.MaxLength)
	putInt(m, "minItems", s.MinItems)
	putInt(m, "maxItems", s.MaxItems)
	if s.Items != nil {
		m["items"] = emitSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for k, v := range s.Properties {
			props[k] = emitSchema(v)
		}
		m["properties"] = props
	}
	if len(s.Required) > 0 {
		m["required"] = toAnySlice(s.Required)
	}
	if s.AdditionalProperties != nil {
		if ap := emitSchema(s.AdditionalProperties); len(ap) == 0 {
			m["additionalProperties"] = true // empty schema ≡ true: any value
		} else {
			m["additionalProperties"] = ap
		}
	}
	maps.Copy(m, s.Extensions)
	return m
}

func emitSecuritySchemes(schemes map[string]SecurityScheme, is32 bool) map[string]any {
	out := map[string]any{}
	for name := range schemes {
		s := schemes[name]
		m := map[string]any{"type": s.Type}
		put(m, "description", s.Description)
		put(m, "scheme", s.Scheme)
		put(m, "bearerFormat", s.BearerFormat)
		put(m, "in", s.In)
		put(m, "name", s.Name)
		put(m, "openIdConnectUrl", s.OpenIDConnectURL)
		if is32 {
			put(m, "oauth2MetadataUrl", s.OAuth2MetadataURL)
			if s.Deprecated {
				m["deprecated"] = true
			}
		}
		if flows := emitFlows(s.Flows, is32); len(flows) > 0 {
			m["flows"] = flows
		}
		out[name] = m
	}
	return out
}

func emitFlows(in map[string]OAuthFlow, is32 bool) map[string]any {
	flows := map[string]any{}
	for fname, f := range in {
		if fname == "deviceAuthorization" && !is32 {
			continue // 3.2-only flow; 3.1 has no valid downgrade
		}
		fm := map[string]any{}
		put(fm, "authorizationUrl", f.AuthorizationURL)
		put(fm, "tokenUrl", f.TokenURL)
		put(fm, "refreshUrl", f.RefreshURL)
		if is32 {
			put(fm, "deviceAuthorizationUrl", f.DeviceAuthorizationURL)
		}
		if f.Scopes != nil {
			fm["scopes"] = toAnyMap(f.Scopes)
		} else {
			fm["scopes"] = map[string]any{}
		}
		flows[fname] = fm
	}
	return flows
}

func emitSecurityReqs(reqs []map[string][]string) []any {
	var out []any
	for _, req := range reqs {
		m := map[string]any{}
		for name, scopes := range req {
			m[name] = toAnySlice(scopes)
		}
		out = append(out, m)
	}
	return out
}

// --- attribute application ----------------------------------------------

// applyAttrs attaches swag trailing attributes (`minimum(1) default(20)`) to a
// parameter's schema. Scalar values are coerced to the schema's type.
func applyAttrs(s *Schema, attrs map[string]string) {
	if s == nil || len(attrs) == 0 {
		return
	}
	for k, v := range attrs {
		switch k {
		case "enums":
			target := enumTarget(s)
			for e := range strings.SplitSeq(v, ",") {
				target.Enum = append(target.Enum, coerceScalar(strings.TrimSpace(e), target))
			}
		case "default":
			s.Default = coerceScalar(v, s)
		case "example":
			s.Example = coerceScalar(v, s)
		case "format":
			s.Format = v
		case "pattern":
			s.Pattern = v
		case "minimum":
			s.Minimum = parseFloatPtr(v)
		case "maximum":
			s.Maximum = parseFloatPtr(v)
		case "multipleof":
			s.MultipleOf = parseFloatPtr(v)
		case "minlength":
			s.MinLength = parseIntPtr(v)
		case "maxlength":
			s.MaxLength = parseIntPtr(v)
		case "minitems":
			s.MinItems = parseIntPtr(v)
		case "maxitems":
			s.MaxItems = parseIntPtr(v)
		}
	}
}

// --- tiny helpers --------------------------------------------------------

func put(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func putFloat(m map[string]any, k string, v *float64) {
	if v != nil {
		m[k] = *v
	}
}

func putInt(m map[string]any, k string, v *int) {
	if v != nil {
		m[k] = *v
	}
}

// responseDescription defaults a missing description to the HTTP status text
// (matching swag) — the spec requires a description on every response.
func responseDescription(r *Response) string {
	if r.Description != "" {
		return r.Description
	}
	if code, err := strconv.Atoi(r.Code); err == nil {
		return http.StatusText(code)
	}
	return ""
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func toAnyMap(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

var standardMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

func isStandardMethod(m string) bool { return standardMethods[m] }

// sequentialMedia are the 3.2 sequential media types, where a response is a
// stream of items and itemSchema describes each one.
var sequentialMedia = map[string]bool{
	"text/event-stream":        true,
	"application/jsonl":        true,
	"application/x-ndjson":     true,
	"application/json-seq":     true,
	"application/geo+json-seq": true,
}

func isSequentialMedia(mt string) bool { return sequentialMedia[mt] }
