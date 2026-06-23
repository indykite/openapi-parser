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

import "strings"

// Parse reads annotated Go files under dirs and builds the API model.
func Parse(dirs []string) (*API, error) {
	src, err := extract(dirs)
	if err != nil {
		return nil, err
	}

	api := &API{
		Schemas:         map[string]*Schema{},
		SecuritySchemes: map[string]SecurityScheme{},
		Extensions:      map[string]any{},
	}
	res := &resolver{src: src, schemas: api.Schemas}

	for _, cg := range src.commentGroups {
		ds := parseCommentGroup(cg.text)
		switch {
		case isOperationGroup(ds):
			api.Operations = append(api.Operations, parseOperation(ds, res, refCtx{file: cg.file}))
		case isGeneralGroup(ds):
			parseGeneral(ds, api)
		}
	}
	return api, nil
}

// generalParser accumulates the general-info section across directives. The
// per-concern apply* methods keep each dispatch small; parseGeneral just routes.
type generalParser struct {
	api        *API
	tags       map[string]*Tag
	schemes    map[string]*SecurityScheme
	lastScheme *SecurityScheme
	tagOrder   []string
}

func parseGeneral(ds []directive, api *API) {
	p := &generalParser{
		api:     api,
		tags:    map[string]*Tag{},
		schemes: map[string]*SecurityScheme{},
	}
	for _, d := range ds {
		switch {
		case p.applyInfo(d):
		case p.applyTag(d):
		case p.applySecurity(d):
		case strings.HasPrefix(d.name, "x-"):
			api.Extensions[d.name] = d.args
		}
	}
	p.finish()
}

// applyInfo handles the info/server/externalDocs directives.
func (p *generalParser) applyInfo(d directive) bool {
	api := p.api
	switch d.name {
	case "title":
		api.Info.Title = d.args
	case "version":
		api.Info.Version = d.args
	case "description":
		api.Info.Description = appendLine(api.Info.Description, d.args)
	case "termsofservice":
		api.Info.TermsOfService = d.args
	case "contact.name":
		api.Info.Contact.Name = d.args
	case "contact.url":
		api.Info.Contact.URL = d.args
	case "contact.email":
		api.Info.Contact.Email = d.args
	case "license.name":
		api.Info.License.Name = d.args
	case "license.url":
		api.Info.License.URL = d.args
	case "host", "basepath":
		// fold host+basepath into a server URL lazily, finalized below
		api.Servers = foldServer(api.Servers, d.name, d.args)
	case "server.url":
		api.Servers = append(api.Servers, Server{URL: d.args})
	case "externaldocs.url":
		ensureExtDocs(api).URL = d.args
	case "externaldocs.description":
		ensureExtDocs(api).Description = d.args
	default:
		return false
	}
	return true
}

// getTag returns the named tag, creating and ordering it on first sight.
func (p *generalParser) getTag(name string) *Tag {
	if t, ok := p.tags[name]; ok {
		return t
	}
	t := &Tag{Name: name}
	p.tags[name] = t
	p.tagOrder = append(p.tagOrder, name)
	return t
}

// applyTag handles the native 3.2 tag directives (no x- smuggling).
func (p *generalParser) applyTag(d directive) bool {
	if d.name == "tag.name" {
		p.getTag(d.args)
		return true
	}
	if !strings.HasPrefix(d.name, "tag.") {
		return false
	}
	// applies to the most recently named tag
	if len(p.tagOrder) == 0 {
		return true
	}
	t := p.tags[p.tagOrder[len(p.tagOrder)-1]]
	switch strings.TrimPrefix(d.name, "tag.") {
	case "summary":
		t.Summary = d.args
	case "description":
		t.Description = d.args
	case "parent":
		t.Parent = d.args
	case "kind":
		t.Kind = d.args
	case "docs.url":
		ensureTagDocs(t).URL = d.args
	case "docs.description":
		ensureTagDocs(t).Description = d.args
	}
	return true
}

// applySecurity handles security-scheme directives. All but the opening
// securitydefinitions.* apply to the most recently declared scheme.
func (p *generalParser) applySecurity(d directive) bool {
	switch {
	case strings.HasPrefix(d.name, "securitydefinitions."):
		p.lastScheme = parseSecurityDef(d, p.schemes)
	case p.lastScheme == nil:
		return false
	case d.name == "in":
		p.lastScheme.In = d.args
	case d.name == "name":
		p.lastScheme.Name = d.args
	case d.name == "tokenurl":
		setFlowField(p.lastScheme, "tokenURL", d.args)
	case d.name == "authorizationurl":
		setFlowField(p.lastScheme, "authorizationURL", d.args)
	case strings.HasPrefix(d.name, "scope."):
		addScope(p.lastScheme, strings.TrimPrefix(d.name, "scope."), d.args)
	default:
		return false
	}
	return true
}

// finish materializes accumulated tags and schemes onto the API in order.
func (p *generalParser) finish() {
	for _, name := range p.tagOrder {
		p.api.Tags = append(p.api.Tags, *p.tags[name])
	}
	for name, s := range p.schemes {
		p.api.SecuritySchemes[name] = *s
	}
}

func parseOperation(ds []directive, res *resolver, ctx refCtx) Operation {
	op := Operation{Extensions: map[string]any{}}
	for _, d := range ds {
		switch d.name {
		case "summary":
			op.Summary = d.args
		case "description":
			op.Description = appendLine(op.Description, d.args)
		case "id":
			op.ID = d.args
		case "tags":
			for t := range strings.SplitSeq(d.args, ",") {
				if t = strings.TrimSpace(t); t != "" {
					op.Tags = append(op.Tags, t)
				}
			}
		case "accept":
			op.Consumes = append(op.Consumes, normalizeMime(d.args))
		case "produce":
			op.Produces = append(op.Produces, normalizeMime(d.args))
		case "deprecated":
			op.Deprecated = true
		case "router":
			op.Path, op.Method = parseRouter(d.args)
		case "param":
			p := parseParam(d.args, res, ctx)
			if p.In == "body" || p.In == "formData" {
				op.Body = &p
			} else {
				op.Params = append(op.Params, p)
			}
		case "success", "failure", "response":
			op.Responses = append(op.Responses, parseResponse(d.args, res, ctx))
		case "security":
			op.Security = append(op.Security, parseSecurityReq(d.args))
		case "header":
			attachHeader(&op, d.args)
		default:
			if strings.HasPrefix(d.name, "x-") {
				op.Extensions[d.name] = d.args
			}
		}
	}
	return op
}

// parseRouter handles `/path/{id} [get]` and the native 3.2 `[query]`.
func parseRouter(args string) (path, method string) {
	args = strings.TrimSpace(args)
	open := strings.LastIndexByte(args, '[')
	if open < 0 {
		return args, "get"
	}
	path = strings.TrimSpace(args[:open])
	method = strings.ToLower(strings.Trim(args[open:], "[] "))
	return path, method
}

// parseParam: `name in type required "desc" attr(...)`.
func parseParam(args string, res *resolver, ctx refCtx) Param {
	toks := fields(args)
	lead, attrs := parseAttributes(toks)
	p := Param{Attributes: attrs}
	// lead = [name in type required desc]
	if len(lead) > 0 {
		p.Name = lead[0]
	}
	if len(lead) > 1 {
		p.In = lead[1]
	}
	if len(lead) > 2 {
		p.Type = lead[2]
	}
	if len(lead) > 3 {
		p.Required = isTruthy(lead[3])
	}
	if len(lead) > 4 {
		p.Description = strings.Join(lead[4:], " ")
	}
	p.Schema = res.schemaForToken(p.Type, ctx)
	return p
}

// parseResponse: `code {kind} dataType "desc"`.
func parseResponse(args string, res *resolver, ctx refCtx) Response {
	toks := fields(args)
	r := Response{Headers: map[string]Header{}}
	if len(toks) > 0 {
		r.Code = toks[0]
	}
	rest := toks[1:]
	if len(rest) > 0 && strings.HasPrefix(rest[0], "{") {
		r.Kind = strings.Trim(rest[0], "{}")
		rest = rest[1:]
	}
	if len(rest) > 0 {
		r.DataType = rest[0]
		rest = rest[1:]
	}
	if len(rest) > 0 {
		r.Description = strings.Join(rest, " ")
	}
	r.Schema = res.schemaForResponse(r.Kind, r.DataType, ctx)
	return r
}

func parseSecurityReq(args string) map[string][]string {
	req := map[string][]string{}
	// "OAuth2Application[write, admin]" or "ApiKeyAuth"
	name := args
	var scopes []string
	if open := strings.IndexByte(args, '['); open >= 0 {
		name = strings.TrimSpace(args[:open])
		inner := strings.Trim(args[open:], "[] ")
		for s := range strings.SplitSeq(inner, ",") {
			if s = strings.TrimSpace(s); s != "" {
				scopes = append(scopes, s)
			}
		}
	}
	req[name] = scopes
	return req
}

func attachHeader(op *Operation, args string) {
	// `code {type} Name "desc"` — attach to matching responses.
	toks := fields(args)
	if len(toks) < 3 {
		return
	}
	code := toks[0]
	typ := strings.Trim(toks[1], "{}")
	name := toks[2]
	desc := ""
	if len(toks) > 3 {
		desc = strings.Join(toks[3:], " ")
	}
	for i := range op.Responses {
		if op.Responses[i].Code == code || code == "all" {
			op.Responses[i].Headers[name] = Header{Type: typ, Description: desc}
		}
	}
}

// --- small general-info helpers ---

func appendLine(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "\n" + add
}

func ensureExtDocs(api *API) *ExternalDocs {
	if api.ExternalDocs == nil {
		api.ExternalDocs = &ExternalDocs{}
	}
	return api.ExternalDocs
}

func ensureTagDocs(t *Tag) *ExternalDocs {
	if t.ExternalDocs == nil {
		t.ExternalDocs = &ExternalDocs{}
	}
	return t.ExternalDocs
}

// foldServer accumulates host/basepath into a single server URL.
func foldServer(servers []Server, key, val string) []Server {
	if len(servers) == 0 {
		servers = []Server{{}}
	}
	switch key {
	case "host":
		servers[0].URL = "//" + val + servers[0].URL
	case "basepath":
		servers[0].URL += val
	}
	return servers
}

func parseSecurityDef(d directive, schemes map[string]*SecurityScheme) *SecurityScheme {
	// d.name like securitydefinitions.basic / .apikey / .oauth2.application
	parts := strings.Split(strings.TrimPrefix(d.name, "securitydefinitions."), ".")
	name := strings.TrimSpace(d.args)
	s := &SecurityScheme{Flows: map[string]OAuthFlow{}}
	switch parts[0] {
	case "basic":
		s.Type, s.Scheme = "http", "basic"
	case "apikey":
		s.Type = "apiKey"
	case "oauth2":
		s.Type = "oauth2"
		if len(parts) > 1 {
			s.Flows[oauthFlowName(parts[1])] = OAuthFlow{Scopes: map[string]string{}}
		}
	}
	schemes[name] = s
	return s
}

func oauthFlowName(swagName string) string {
	switch swagName {
	case "application":
		return "clientCredentials"
	case "implicit":
		return "implicit"
	case "password":
		return "password"
	case "accesscode":
		return "authorizationCode"
	}
	return swagName
}

func setFlowField(s *SecurityScheme, field, val string) {
	for k, f := range s.Flows {
		switch field {
		case "tokenURL":
			f.TokenURL = val
		case "authorizationURL":
			f.AuthorizationURL = val
		}
		s.Flows[k] = f
	}
}

func addScope(s *SecurityScheme, scope, desc string) {
	for k, f := range s.Flows {
		if f.Scopes == nil {
			f.Scopes = map[string]string{}
		}
		f.Scopes[scope] = desc
		s.Flows[k] = f
	}
}
