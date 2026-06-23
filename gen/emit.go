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
	if len(api.Servers) > 0 {
		doc["servers"] = emitServers(api.Servers)
	}
	if len(api.Tags) > 0 {
		doc["tags"] = emitTags(api.Tags, is32)
	}
	if api.ExternalDocs != nil {
		doc["externalDocs"] = emitExtDocs(api.ExternalDocs)
	}

	doc["paths"] = emitPaths(api.Operations, api.Produces(), is32)

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
		put(lm, "url", l.URL)
		m["license"] = lm
	}
	return m
}

func emitServers(servers []Server) []any {
	out := make([]any, 0, len(servers))
	for _, s := range servers {
		url := s.URL
		// foldServer may leave a protocol-relative // prefix; default to https.
		if strings.HasPrefix(url, "//") {
			url = "https:" + url
		}
		sm := map[string]any{"url": url}
		put(sm, "description", s.Description)
		out = append(out, sm)
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
		opObj := emitOperation(op, produce, is32)

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
	return paths
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
	if len(op.Params) > 0 {
		var params []any
		for i := range op.Params {
			params = append(params, emitParam(&op.Params[i]))
		}
		m["parameters"] = params
	}
	if op.Body != nil {
		ct := "application/json"
		// (could derive from op.Consumes)
		m["requestBody"] = map[string]any{
			"required": op.Body.Required,
			"content": map[string]any{
				ct: map[string]any{"schema": emitSchema(op.Body.Schema)},
			},
		}
	}
	m["responses"] = emitResponses(op, produce, is32)
	if len(op.Security) > 0 {
		m["security"] = emitSecurityReqs(op.Security)
	}
	maps.Copy(m, op.Extensions)
	return m
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
	schema := p.Schema
	if schema == nil {
		schema = &Schema{Type: []string{"string"}}
	}
	applyAttrs(schema, p.Attributes)
	m["schema"] = emitSchema(schema)
	return m
}

func emitResponses(op *Operation, produce string, is32 bool) map[string]any {
	out := map[string]any{}
	if len(op.Produces) > 0 {
		produce = op.Produces[0]
	}
	for _, r := range op.Responses {
		respObj := map[string]any{"description": orDefault(r.Description, "")}
		if r.Schema != nil {
			schemaKey := "schema"
			if op.Streaming && is32 {
				schemaKey = "itemSchema" // 3.2 streaming
			}
			respObj["content"] = map[string]any{
				produce: map[string]any{schemaKey: emitSchema(r.Schema)},
			}
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
	if len(s.Type) == 1 {
		m["type"] = s.Type[0]
	} else if len(s.Type) > 1 {
		m["type"] = toAnySlice(s.Type) // 3.1/3.2 type array
	}
	put(m, "format", s.Format)
	put(m, "description", s.Description)
	if s.Example != nil {
		m["example"] = s.Example
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
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
		m["additionalProperties"] = emitSchema(s.AdditionalProperties)
	}
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
		if len(s.Flows) > 0 {
			flows := map[string]any{}
			for fname, f := range s.Flows {
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
			m["flows"] = flows
		}
		out[name] = m
	}
	return out
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

func applyAttrs(s *Schema, attrs map[string]string) {
	if s == nil || len(attrs) == 0 {
		return
	}
	if v, ok := attrs["enums"]; ok {
		for e := range strings.SplitSeq(v, ",") {
			s.Enum = append(s.Enum, strings.TrimSpace(e))
		}
	}
	if v, ok := attrs["default"]; ok {
		s.Example = v // TODO: a real Default field on Schema
	}
	if v, ok := attrs["example"]; ok {
		s.Example = v
	}
	if v, ok := attrs["format"]; ok {
		s.Format = v
	}
	// numeric/length attributes (minimum, maximum, minlength, ...) are parsed
	// but not yet attached — TODO: extend Schema with those fields.
}

// --- tiny helpers --------------------------------------------------------

func put(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func orDefault(s, _ string) string {
	if s == "" {
		return "" // description is required by spec; emitter leaves "" so
		// validation flags it rather than inventing text.
	}
	return s
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
