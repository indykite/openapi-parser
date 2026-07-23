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

	var fns []string // handler func name per operation, for operationId defaulting
	for _, cg := range src.commentGroups {
		ds := parseCommentGroup(cg.text)
		switch {
		case isOperationGroup(ds):
			api.Operations = append(api.Operations, parseOperation(ds, res, refCtx{file: cg.file}))
			fns = append(fns, cg.fn)
		case isGeneralGroup(ds):
			parseGeneral(ds, api)
		}
	}
	defaultOperationIDs(api.Operations, fns)
	return api, nil
}

// defaultOperationIDs fills missing operationIds from the documented handler
// function names — but only when the name is unambiguous, since operationIds
// must be unique across the document (methods on different receivers can
// share a name).
func defaultOperationIDs(ops []Operation, fns []string) {
	taken := map[string]int{}
	for i := range ops {
		if ops[i].ID != "" {
			taken[ops[i].ID]++
		}
	}
	candidates := map[string]int{}
	for i := range ops {
		if ops[i].ID == "" && fns[i] != "" {
			candidates[fns[i]]++
		}
	}
	for i := range ops {
		if ops[i].ID == "" && fns[i] != "" && candidates[fns[i]] == 1 && taken[fns[i]] == 0 {
			ops[i].ID = fns[i]
		}
	}
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
		// security first: once a scheme is declared, positional follow-ons
		// like @description belong to it. Any non-security directive ends the
		// scheme section (swag's scan stops there too), so a later
		// @description belongs to info again.
		switch {
		case p.applySecurity(d):
			continue
		case p.applyTag(d):
		case p.applyInfo(d):
		case strings.HasPrefix(d.name, "x-"):
			api.Extensions[d.name] = d.args
		}
		p.lastScheme = nil
	}
	p.finish()
}

// applyInfo handles the info/server/externalDocs directives.
func (p *generalParser) applyInfo(d directive) bool {
	api := p.api
	switch d.name {
	case "title":
		api.Info.Title = d.args
	case "summary":
		api.Info.Summary = d.args // 3.1+
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
	case "license.identifier":
		api.Info.License.Identifier = d.args // 3.1+ SPDX expression
	case "license.url":
		api.Info.License.URL = d.args
	case "host", "basepath":
		// fold host+basepath into a server URL lazily, finalized below
		api.Servers = foldServer(api.Servers, d.name, d.args)
	case "schemes":
		// space-separated: `@schemes https http`
		api.Schemes = append(api.Schemes, strings.Fields(d.args)...)
	case "server.url":
		api.Servers = append(api.Servers, Server{URL: d.args})
	case "server.name":
		lastServer(api).Name = d.args // 3.2
	case "server.description":
		lastServer(api).Description = d.args
	case "server.variable":
		addServerVariable(lastServer(api), d.args)
	case "self":
		api.Self = d.args // 3.2 $self document URI
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
// securitydefinitions.* and the document-level @security apply to the most
// recently declared scheme.
func (p *generalParser) applySecurity(d directive) bool {
	switch {
	case strings.HasPrefix(d.name, "securitydefinitions."):
		p.lastScheme = parseSecurityDef(d, p.schemes)
	case d.name == "security":
		// document-level security requirement — not a scheme attribute, so
		// it ends the scheme section like any other non-scheme directive
		if req := parseSecurityReq(d.args); len(req) > 0 {
			p.api.Security = append(p.api.Security, req)
		}
		p.lastScheme = nil
	case p.lastScheme == nil:
		return false
	case d.name == "in":
		p.lastScheme.In = d.args
	case d.name == "name":
		p.lastScheme.Name = d.args
	case d.name == "bearerformat":
		p.lastScheme.BearerFormat = d.args
	case d.name == "openidconnecturl":
		p.lastScheme.OpenIDConnectURL = d.args
	case d.name == "description":
		p.lastScheme.Description = appendLine(p.lastScheme.Description, d.args)
	case d.name == "tokenurl":
		setFlowField(p.lastScheme, "tokenURL", d.args)
	case d.name == "authorizationurl":
		setFlowField(p.lastScheme, "authorizationURL", d.args)
	case d.name == "refreshurl":
		setFlowField(p.lastScheme, "refreshURL", d.args)
	case d.name == "deviceauthorizationurl":
		setFlowField(p.lastScheme, "deviceAuthorizationURL", d.args)
	case d.name == "oauth2metadataurl":
		p.lastScheme.OAuth2MetadataURL = d.args
	case d.name == "deprecated":
		p.lastScheme.Deprecated = true
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
			op.Consumes = append(op.Consumes, splitMimeList(d.args)...)
		case "produce":
			op.Produces = append(op.Produces, splitMimeList(d.args)...)
		case "deprecated":
			op.Deprecated = true
		case "router":
			op.Path, op.Method = parseRouter(d.args)
		case "webhook":
			// `@Webhook name [method]`, same shape as @Router; webhooks are
			// event deliveries, so the method defaults to post.
			op.Webhook, op.Method = parseRouter(d.args)
			if !strings.Contains(d.args, "[") {
				op.Method = "post"
			}
		case "param":
			p := parseParam(d.args, res, ctx)
			switch p.In {
			case "body":
				op.Body = &p
			case "formData":
				op.Form = append(op.Form, p)
			default:
				op.Params = append(op.Params, p)
			}
		case "success", "failure", "response":
			op.Responses = append(op.Responses, parseResponses(d.args, res, ctx)...)
		case "security":
			if req := parseSecurityReq(d.args); len(req) > 0 {
				op.Security = append(op.Security, req)
			}
		case "header":
			attachHeader(&op, d.args)
		case "externaldocs.url":
			ensureOpExtDocs(&op).URL = d.args
		case "externaldocs.description":
			ensureOpExtDocs(&op).Description = d.args
		case "responsesummary":
			// `@ResponseSummary code text` — 3.2 response summary; like
			// @Header, it must follow the @Success/@Failure line it targets.
			attachResponseSummary(&op, d.args)
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
	// schema-shaped attributes attach here, once; emit stays side-effect-free
	applyAttrs(p.Schema, p.Attributes)
	return p
}

// parseResponses: `code {kind} dataType "desc"` — kind, dataType, and desc
// are each optional (`@Success 204 "no content"` has neither kind nor type).
// The raw string is scanned so a quoted description is never mistaken for a
// type. The code may be a comma list (`@Failure 400,404 ...`), producing one
// response per code.
func parseResponses(args string, res *resolver, ctx refCtx) []Response {
	r := Response{}
	var codes string
	codes, args = splitFirst(args)
	if strings.HasPrefix(args, "{") {
		if end := strings.IndexByte(args, '}'); end > 0 {
			r.Kind = args[1:end]
			args = strings.TrimSpace(args[end+1:])
		}
	}
	if args != "" && !strings.HasPrefix(args, `"`) {
		r.DataType, args = splitFirst(args)
	}
	r.Description = strings.Trim(args, `" `)
	r.Schema = res.schemaForResponse(r.Kind, r.DataType, ctx)

	var out []Response
	for code := range strings.SplitSeq(codes, ",") {
		if code = strings.TrimSpace(code); code == "" {
			continue
		}
		resp := r
		resp.Code = code
		resp.Headers = map[string]Header{} // each response owns its headers
		out = append(out, resp)
	}
	return out
}

// splitMimeList handles swag's comma-separated @Accept/@Produce lists.
func splitMimeList(args string) []string {
	var out []string
	for mt := range strings.SplitSeq(args, ",") {
		if mt = strings.TrimSpace(mt); mt != "" {
			out = append(out, normalizeMime(mt))
		}
	}
	return out
}

func parseSecurityReq(args string) map[string][]string {
	req := map[string][]string{}
	// One or more schemes combined with && (all required together, swag syntax):
	// "OAuth2Application[write, admin]", "ApiKeyAuth" or "ApiKeyAuth && BearerAuth".
	for part := range strings.SplitSeq(args, "&&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		var scopes []string
		if open := strings.IndexByte(part, '['); open >= 0 {
			name = strings.TrimSpace(part[:open])
			inner := strings.Trim(part[open:], "[] ")
			for s := range strings.SplitSeq(inner, ",") {
				if s = strings.TrimSpace(s); s != "" {
					scopes = append(scopes, s)
				}
			}
		}
		if name == "" {
			continue // malformed scope-only part like "[write]" - no scheme to require
		}
		req[name] = scopes
	}
	if len(req) == 0 {
		// An empty Security Requirement Object ({}) would mean "no auth
		// required" - never emit one for a malformed annotation.
		return nil
	}
	return req
}

func attachHeader(op *Operation, args string) {
	// `code {type} Name "desc"` - attach to matching responses.
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

func ensureOpExtDocs(op *Operation) *ExternalDocs {
	if op.ExternalDocs == nil {
		op.ExternalDocs = &ExternalDocs{}
	}
	return op.ExternalDocs
}

func attachResponseSummary(op *Operation, args string) {
	code, text := splitFirst(args)
	text = strings.Trim(text, `" `)
	for i := range op.Responses {
		if op.Responses[i].Code == code {
			op.Responses[i].Summary = text
		}
	}
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
	case "bearerauth", "bearer":
		s.Type, s.Scheme = "http", "bearer"
	case "apikey":
		s.Type = "apiKey"
	case "openidconnect":
		s.Type = "openIdConnect"
	case "mutualtls":
		s.Type = "mutualTLS" // 3.1+
	case "oauth2":
		s.Type = "oauth2"
		if len(parts) > 1 {
			s.Flows[oauthFlowName(parts[1])] = OAuthFlow{Scopes: map[string]string{}}
		}
	}
	schemes[name] = s
	return s
}

// lastServer returns the most recent server entry, creating one if needed so
// server.* follow-on directives always have a target.
func lastServer(api *API) *Server {
	if len(api.Servers) == 0 {
		api.Servers = []Server{{}}
	}
	return &api.Servers[len(api.Servers)-1]
}

// addServerVariable parses `@server.variable name default "desc" enums(a,b)`
// onto a server.
func addServerVariable(s *Server, args string) {
	lead, attrs := parseAttributes(fields(args))
	if len(lead) < 2 {
		return // name and default are required by the spec
	}
	v := ServerVariable{Default: lead[1]}
	if len(lead) > 2 {
		v.Description = strings.Join(lead[2:], " ")
	}
	if en, ok := attrs["enums"]; ok {
		for e := range strings.SplitSeq(en, ",") {
			if e = strings.TrimSpace(e); e != "" {
				v.Enum = append(v.Enum, e)
			}
		}
	}
	if s.Variables == nil {
		s.Variables = map[string]ServerVariable{}
	}
	s.Variables[lead[0]] = v
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
	case "device":
		return "deviceAuthorization" // 3.2 device authorization grant
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
		case "refreshURL":
			f.RefreshURL = val
		case "deviceAuthorizationURL":
			f.DeviceAuthorizationURL = val
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
