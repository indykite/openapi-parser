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

package gen_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/indykite/openapi-parser/gen"
)

func TestParseAndEmit(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}

	if api.Info.Title != "Example API" {
		t.Errorf("title: %q", api.Info.Title)
	}
	if len(api.Operations) != 10 {
		t.Fatalf("want 10 operations (incl. webhook), got %d", len(api.Operations))
	}
	if _, ok := api.Schemas["testdata.Account"]; !ok {
		t.Error("Account schema not resolved")
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})

	if doc["openapi"] != "3.2.0" {
		t.Errorf("version: %v", doc["openapi"])
	}

	paths := doc["paths"].(map[string]any)

	// QUERY method should be first-class in 3.2.
	search := paths["/accounts/search"].(map[string]any)
	if _, ok := search["query"]; !ok {
		t.Errorf("expected query operation, got keys: %v", keysOf(search))
	}

	// Hierarchical tag must carry native parent/kind.
	tags := doc["tags"].([]any)
	var found bool
	for _, raw := range tags {
		tg := raw.(map[string]any)
		if tg["name"] == "accounts.reports" {
			found = true
			if tg["parent"] != "accounts" {
				t.Errorf("parent not emitted: %v", tg)
			}
			if tg["kind"] != "nav" {
				t.Errorf("kind not emitted: %v", tg)
			}
		}
	}
	if !found {
		t.Error("hierarchical tag missing")
	}

	// Schema required/enums.
	acct := doc["components"].(map[string]any)["schemas"].(map[string]any)["testdata.Account"].(map[string]any)
	req, _ := acct["required"].([]any)
	if !containsAny(req, "name") {
		t.Errorf("name should be required: %v", req)
	}
}

func TestCrossPackageAndComposition(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}

	// The type from the OTHER package must be resolved and registered.
	if _, ok := api.Schemas["model.Thing"]; !ok {
		t.Fatalf("cross-package type model.Thing not resolved; have: %v", schemaKeys(api.Schemas))
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	paths := doc["paths"].(map[string]any)
	get := paths["/wrapped"].(map[string]any)["get"].(map[string]any)
	resp := get["responses"].(map[string]any)

	// 200: Envelope{data=model.Thing} -> inline object, data overridden to $ref.
	schema200 := contentSchema(resp["200"])
	props := schema200["properties"].(map[string]any)
	if _, ok := props["code"]; !ok {
		t.Error("base Envelope property 'code' missing from composition")
	}
	data := props["data"].(map[string]any)
	if data["$ref"] != "#/components/schemas/model.Thing" {
		t.Errorf("data not overridden to cross-package ref: %v", data)
	}

	// 201: Envelope{data=[]model.Thing} -> data is an array of refs.
	schema201 := contentSchema(resp["201"])
	data201 := schema201["properties"].(map[string]any)["data"].(map[string]any)
	if data201["type"] != "array" {
		t.Errorf("expected array data, got: %v", data201)
	}
	items := data201["items"].(map[string]any)
	if items["$ref"] != "#/components/schemas/model.Thing" {
		t.Errorf("array items not a cross-package ref: %v", items)
	}
}

func contentSchema(resp any) map[string]any {
	r := resp.(map[string]any)
	content := r["content"].(map[string]any)
	for _, mt := range content {
		return mt.(map[string]any)["schema"].(map[string]any)
	}
	return nil
}

func schemaKeys(m map[string]*gen.Schema) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestSchemesExpandServers(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	servers := doc["servers"].([]any)
	var urls []string
	for _, s := range servers {
		urls = append(urls, s.(map[string]any)["url"].(string))
	}
	want := []string{
		"https://api.example.com/v1",
		"http://api.example.com/v1",
		"https://{region}.api.example.com/v1",
	}
	for _, w := range want {
		if !slices.Contains(urls, w) {
			t.Errorf("missing server %q, have %v", w, urls)
		}
	}
	if len(urls) != 3 {
		t.Errorf("want 3 servers (2 scheme-expanded + 1 explicit), got %v", urls)
	}
}

func TestMultiWordSecuritySchemeName(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := api.SecuritySchemes["Bearer Token"]; !ok {
		t.Fatalf("multi-word scheme name not parsed; have: %v", slices.Collect(maps.Keys(api.SecuritySchemes)))
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	list := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	sec := list["security"].([]any)[0].(map[string]any)
	if _, ok := sec["Bearer Token"]; !ok {
		t.Errorf("operation security requirement lost the multi-word name: %v", sec)
	}
}

func TestGenericInstantiation(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	const instance = "testdata.listResponse-testdata.AccountResponse"
	inst, ok := api.Schemas[instance]
	if !ok {
		t.Fatalf("generic instantiation not registered; have: %v", schemaKeys(api.Schemas))
	}
	data := inst.Properties["data"]
	if data == nil || !slices.Contains(data.Type, "array") {
		t.Fatalf("data should be an array, got %+v", data)
	}
	if data.Items == nil || data.Items.Ref != "#/components/schemas/testdata.AccountResponse" {
		t.Errorf("data items should ref the type argument, got %+v", data.Items)
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	schema := contentSchema(get["responses"].(map[string]any)["200"])
	if schema["$ref"] != "#/components/schemas/"+instance {
		t.Errorf("response should ref the instantiation: %v", schema)
	}
}

func TestEmbeddedStructPromotion(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	acct, ok := api.Schemas["testdata.AccountResponse"]
	if !ok {
		t.Fatalf("AccountResponse not resolved; have: %v", schemaKeys(api.Schemas))
	}
	for _, prop := range []string{"id", "etag", "name"} {
		if acct.Properties[prop] == nil {
			t.Errorf("promoted/own property %q missing; have %v", prop, slices.Collect(maps.Keys(acct.Properties)))
		}
	}
	if !slices.Contains(acct.Required, "id") {
		t.Errorf("embedded required field should promote, got %v", acct.Required)
	}

	// Named non-struct type (type Labels []*Account) resolves inline; the
	// pointer element makes items nullable via anyOf ($ref allows no siblings).
	labels := acct.Properties["labels"]
	if labels == nil || !slices.Contains(labels.Type, "array") {
		t.Fatalf("labels should inline the named slice type as array, got %+v", labels)
	}
	items := labels.Items
	if items == nil || len(items.AnyOf) != 2 {
		t.Fatalf("pointer items should be anyOf[$ref, null], got %+v", items)
	}
	if items.AnyOf[0].Ref != "#/components/schemas/testdata.Account" {
		t.Errorf("anyOf[0] should ref Account, got %+v", items.AnyOf[0])
	}
	if !slices.Contains(items.AnyOf[1].Type, "null") {
		t.Errorf("anyOf[1] should be null, got %+v", items.AnyOf[1])
	}
}

func TestParamConstraintAttributes(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	limit := get["parameters"].([]any)[0].(map[string]any)
	schema := limit["schema"].(map[string]any)
	if schema["minimum"] != 1.0 || schema["maximum"] != 100.0 {
		t.Errorf("minimum/maximum not applied: %v", schema)
	}
	if def, ok := schema["default"].(int64); !ok || def != 20 {
		t.Errorf("default should be typed int 20, got %T %v", schema["default"], schema["default"])
	}
}

func TestStructTagConstraints(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	acct := api.Schemas["testdata.Account"]

	// example:"1" on an int field is typed.
	if id, ok := acct.Properties["id"].Example.(int64); !ok || id != 1 {
		t.Errorf("id example should be int64 1, got %T %v", acct.Properties["id"].Example, acct.Properties["id"].Example)
	}
	// enums on an int field coerce; minimum/maximum tags apply.
	prio := acct.Properties["priority"]
	if len(prio.Enum) != 3 || prio.Enum[0] != int64(1) {
		t.Errorf("priority enum should be typed ints, got %v", prio.Enum)
	}
	if prio.Minimum == nil || *prio.Minimum != 1 || prio.Maximum == nil || *prio.Maximum != 3 {
		t.Errorf("priority minimum/maximum tags not applied: %+v", prio)
	}
	// binding oneof becomes enum; binding required joins required.
	status := acct.Properties["status"]
	if len(status.Enum) != 2 || status.Enum[0] != "active" || status.Enum[1] != "inactive" {
		t.Errorf("oneof should map to enum, got %v", status.Enum)
	}
	if !slices.Contains(acct.Required, "status") {
		t.Errorf("binding required missing from required: %v", acct.Required)
	}
	// gte/lte on a number map to minimum/maximum.
	weight := acct.Properties["weight"]
	if weight.Minimum == nil || *weight.Minimum != 0 || weight.Maximum == nil || *weight.Maximum != 1 {
		t.Errorf("gte/lte not mapped: %+v", weight)
	}
}

func TestEscapedTagsUnexportedDiveRequired(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	acct := api.Schemas["testdata.Account"]

	// Escaped quotes in the example must not truncate it or eat later tags.
	config := acct.Properties["config"]
	if config.Example != `{"key":"value"}` {
		t.Errorf("escaped example mangled: %#v", config.Example)
	}
	if config.MinLength == nil || *config.MinLength != 1 || config.MaxLength == nil || *config.MaxLength != 100 {
		t.Errorf("tags after escaped example lost: %+v", config)
	}

	// required wins over json omitempty, like swag.
	for _, want := range []string{"config", "code"} {
		if !slices.Contains(acct.Required, want) {
			t.Errorf("%s should be required, got %v", want, acct.Required)
		}
	}

	// dive scopes the second min to elements.
	hosts := acct.Properties["hosts"]
	if hosts.MinItems == nil || *hosts.MinItems != 1 {
		t.Errorf("hosts minItems should be 1: %+v", hosts)
	}
	if hosts.MaxItems != nil {
		t.Errorf("hosts must have no maxItems: %+v", hosts)
	}
	if hosts.Items == nil || hosts.Items.MinLength == nil || *hosts.Items.MinLength != 8 {
		t.Errorf("dive should put min=8 on items.minLength: %+v", hosts.Items)
	}

	// unexported fields are never marshaled.
	if _, ok := acct.Properties["hidden"]; ok {
		t.Error("unexported field must not appear in the schema")
	}
}

func TestQuotedDescriptionWithParens(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	for _, raw := range get["parameters"].([]any) {
		p := raw.(map[string]any)
		if p["name"] != "page" {
			continue
		}
		if p["description"] != "page number (required when using cursor)" {
			t.Errorf("parenthesized description eaten: %v", p["description"])
		}
		return
	}
	t.Error("page param not found")
}

func TestRequestBodyContentType(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	search := doc["paths"].(map[string]any)["/accounts/search"].(map[string]any)["query"].(map[string]any)
	body := search["requestBody"].(map[string]any)
	content := body["content"].(map[string]any)
	// comma-separated @Accept list: one content entry per media type
	for _, mt := range []string{"text/xml", "application/json"} {
		if _, ok := content[mt]; !ok {
			t.Errorf("@Accept xml,json should produce %s, got %v", mt, slices.Collect(maps.Keys(content)))
		}
	}
	if body["description"] != "search filter" {
		t.Errorf("body param description should become requestBody description: %v", body["description"])
	}
}

func TestFormDataBody(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	post := doc["paths"].(map[string]any)["/accounts/{id}/avatar"].(map[string]any)["post"].(map[string]any)
	body := post["requestBody"].(map[string]any)
	if req, ok := body["required"].(bool); !ok || !req {
		t.Errorf("form with a required field should be required: %v", body)
	}
	mt := body["content"].(map[string]any)["multipart/form-data"].(map[string]any)
	schema := mt["schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	avatar, ok := props["avatar"].(map[string]any)
	if !ok || avatar["type"] != "string" || avatar["format"] != "binary" {
		t.Errorf("file form field should be string/binary: %v", props["avatar"])
	}
	if _, ok := props["label"]; !ok {
		t.Errorf("all form fields must survive, got %v", slices.Collect(maps.Keys(props)))
	}
	req, _ := schema["required"].([]any)
	if len(req) != 1 || req[0] != "avatar" {
		t.Errorf("only avatar is required: %v", req)
	}
}

func TestCommaStatusCodes(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	show := doc["paths"].(map[string]any)["/accounts/{id}"].(map[string]any)["get"].(map[string]any)
	responses := show["responses"].(map[string]any)
	for _, code := range []string{"401", "403"} {
		r, ok := responses[code].(map[string]any)
		if !ok {
			t.Fatalf("code %s missing from %v", code, keysOf(responses))
		}
		if r["description"] != "denied" {
			t.Errorf("%s description: %v", code, r["description"])
		}
	}
	if _, ok := responses["401,403"]; ok {
		t.Error("comma list must not become a literal response key")
	}
}

func TestSpacedEnumsAttribute(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	for _, raw := range get["parameters"].([]any) {
		p := raw.(map[string]any)
		if p["name"] != "state" {
			continue
		}
		if p["description"] != "State filter" {
			t.Errorf("attribute fragments leaked into description: %v", p["description"])
		}
		enum, _ := p["schema"].(map[string]any)["enum"].([]any)
		if len(enum) != 2 || enum[0] != "active" || enum[1] != "suspended" {
			t.Errorf("Enums(active, suspended) should parse: %v", enum)
		}
		return
	}
	t.Error("state param not found")
}

func TestOperationIDCollisionLeftEmpty(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	for _, path := range []string{"/alpha/stats", "/beta/stats"} {
		op := doc["paths"].(map[string]any)[path].(map[string]any)["get"].(map[string]any)
		if id, ok := op["operationId"]; ok {
			t.Errorf("%s: ambiguous handler name must not become operationId, got %v", path, id)
		}
	}
}

func TestEmitIsIdempotent(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	stateEnum := func(doc map[string]any) []any {
		get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
		for _, raw := range get["parameters"].([]any) {
			if p := raw.(map[string]any); p["name"] == "state" {
				enum, _ := p["schema"].(map[string]any)["enum"].([]any)
				return enum
			}
		}
		return nil
	}
	first := stateEnum(api.Emit(gen.EmitOptions{Version: "3.2.0"}))
	second := stateEnum(api.Emit(gen.EmitOptions{Version: "3.1.0"}))
	third := stateEnum(api.Emit(gen.EmitOptions{Version: "3.2.0"}))
	if len(first) != 2 || len(second) != 2 || len(third) != 2 {
		t.Errorf("enum must not grow across emits: %d, %d, %d", len(first), len(second), len(third))
	}
}

func TestStreamingItemSchema(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	watch := doc["paths"].(map[string]any)["/accounts/watch"].(map[string]any)["get"].(map[string]any)
	content := watch["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	sse := content["text/event-stream"].(map[string]any)
	if _, ok := sse["itemSchema"]; !ok {
		t.Errorf("3.2 SSE response should use itemSchema, got keys %v", keysOf(sse))
	}
	if _, ok := sse["schema"]; ok {
		t.Error("3.2 SSE response should not also have schema")
	}

	doc31 := api.Emit(gen.EmitOptions{Version: "3.1.0"})
	watch31 := doc31["paths"].(map[string]any)["/accounts/watch"].(map[string]any)["get"].(map[string]any)
	content31 := watch31["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	sse31 := content31["text/event-stream"].(map[string]any)
	if _, ok := sse31["schema"]; !ok {
		t.Errorf("3.1 downgrade should fall back to schema, got keys %v", keysOf(sse31))
	}
}

func TestWebhooks(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})

	hooks, ok := doc["webhooks"].(map[string]any)
	if !ok {
		t.Fatal("webhooks section missing")
	}
	post, ok := hooks["account.created"].(map[string]any)["post"].(map[string]any)
	if !ok {
		t.Fatalf("webhook should default to post, got %v", hooks["account.created"])
	}
	resp := post["responses"].(map[string]any)["204"].(map[string]any)
	if resp["description"] != "acknowledged" {
		t.Errorf("bare quoted description misparsed: %v", resp)
	}
	if _, ok := resp["content"]; ok {
		t.Errorf("204 should have no content: %v", resp)
	}
	if _, ok := doc["paths"].(map[string]any)["account.created"]; ok {
		t.Error("webhook leaked into paths")
	}
}

func TestSelf(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	if doc["$self"] != "https://api.example.com/v1/openapi.json" {
		t.Errorf("$self not emitted: %v", doc["$self"])
	}
	doc31 := api.Emit(gen.EmitOptions{Version: "3.1.0"})
	if _, ok := doc31["$self"]; ok {
		t.Error("$self is 3.2-only, must not appear in 3.1")
	}
}

func TestOAuth2DeviceFlow(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	dev := schemes["OAuth2Device"].(map[string]any)
	if dev["oauth2MetadataUrl"] != "https://auth.example.com/.well-known/oauth-authorization-server" {
		t.Errorf("oauth2MetadataUrl not emitted: %v", dev)
	}
	flow := dev["flows"].(map[string]any)["deviceAuthorization"].(map[string]any)
	if flow["deviceAuthorizationUrl"] != "https://auth.example.com/device" ||
		flow["tokenUrl"] != "https://auth.example.com/token" {
		t.Errorf("device flow urls wrong: %v", flow)
	}
	if flow["scopes"].(map[string]any)["read"] != "read access" {
		t.Errorf("device flow scopes wrong: %v", flow)
	}

	// 3.1 has no deviceAuthorization flow; with no other flow, flows is dropped.
	doc31 := api.Emit(gen.EmitOptions{Version: "3.1.0"})
	dev31 := doc31["components"].(map[string]any)["securitySchemes"].(map[string]any)["OAuth2Device"].(map[string]any)
	if _, ok := dev31["flows"]; ok {
		t.Errorf("device-only flows should be dropped in 3.1: %v", dev31)
	}
	if _, ok := dev31["oauth2MetadataUrl"]; ok {
		t.Error("oauth2MetadataUrl is 3.2-only")
	}
}

func TestInfoSummaryAndLicenseIdentifier(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	info := api.Emit(gen.EmitOptions{Version: "3.2.0"})["info"].(map[string]any)
	if info["summary"] != "Accounts and things, for parser exercise." {
		t.Errorf("info.summary: %v", info["summary"])
	}
	if info["license"].(map[string]any)["identifier"] != "Apache-2.0" {
		t.Errorf("license.identifier: %v", info["license"])
	}
}

func TestTopLevelSecurity(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	sec, ok := doc["security"].([]any)
	if !ok || len(sec) != 1 {
		t.Fatalf("top-level security missing: %v", doc["security"])
	}
	if _, ok := sec[0].(map[string]any)["ApiKeyAuth"]; !ok {
		t.Errorf("ApiKeyAuth requirement missing: %v", sec[0])
	}
}

func TestSchemeDescriptionRouting(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	// @description after a securitydefinitions block belongs to the scheme...
	if got := api.SecuritySchemes["BearerJWT"].Description; got != "Bearer token for machine clients." {
		t.Errorf("scheme description: %q", got)
	}
	// ...and must not leak into info.description.
	if strings.Contains(api.Info.Description, "machine clients") {
		t.Errorf("scheme description leaked into info: %q", api.Info.Description)
	}
	if api.Info.Description != "A sample API exercising the parser." {
		t.Errorf("info description changed: %q", api.Info.Description)
	}
}

func TestOperationIDFromFuncName(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	paths := doc["paths"].(map[string]any)

	// defaults to the documented handler's name...
	show := paths["/accounts/{id}"].(map[string]any)["get"].(map[string]any)
	if show["operationId"] != "ShowAccount" {
		t.Errorf("operationId should default to func name: %v", show["operationId"])
	}
	// ...but an explicit @ID always wins.
	purge := paths["/accounts/{id}"].(map[string]any)["additionalOperations"].(map[string]any)["PURGE"].(map[string]any)
	if purge["operationId"] != "purgeAccount" {
		t.Errorf("@ID should override func name: %v", purge["operationId"])
	}
}

func TestSecuritySchemeTypes(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	components := api.Emit(gen.EmitOptions{Version: "3.2.0"})["components"].(map[string]any)
	schemes := components["securitySchemes"].(map[string]any)

	bearer := schemes["BearerJWT"].(map[string]any)
	if bearer["type"] != "http" || bearer["scheme"] != "bearer" || bearer["bearerFormat"] != "JWT" {
		t.Errorf("bearer scheme wrong: %v", bearer)
	}
	oidc := schemes["OIDC"].(map[string]any)
	if oidc["type"] != "openIdConnect" ||
		oidc["openIdConnectUrl"] != "https://auth.example.com/.well-known/openid-configuration" {
		t.Errorf("openIdConnect scheme wrong: %v", oidc)
	}
	if schemes["MTLS"].(map[string]any)["type"] != "mutualTLS" {
		t.Errorf("mutualTLS scheme wrong: %v", schemes["MTLS"])
	}
}

func TestServerNameAndVariables(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	findRegional := func(doc map[string]any) map[string]any {
		for _, raw := range doc["servers"].([]any) {
			s := raw.(map[string]any)
			if s["url"] == "https://{region}.api.example.com/v1" {
				return s
			}
		}
		return nil
	}

	s32 := findRegional(api.Emit(gen.EmitOptions{Version: "3.2.0"}))
	if s32["name"] != "regional" || s32["description"] != "Regional endpoint" {
		t.Errorf("server name/description wrong: %v", s32)
	}
	region := s32["variables"].(map[string]any)["region"].(map[string]any)
	if region["default"] != "eu" || region["description"] != "Region code" {
		t.Errorf("server variable wrong: %v", region)
	}
	if enum := region["enum"].([]any); len(enum) != 2 || enum[0] != "eu" || enum[1] != "us" {
		t.Errorf("server variable enum wrong: %v", region)
	}

	s31 := findRegional(api.Emit(gen.EmitOptions{Version: "3.1.0"}))
	if _, ok := s31["name"]; ok {
		t.Error("server name is 3.2-only, must not appear in 3.1")
	}
	if _, ok := s31["variables"]; !ok {
		t.Error("server variables must survive in 3.1")
	}
}

func TestOperationExternalDocs(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	show := doc["paths"].(map[string]any)["/accounts/{id}"].(map[string]any)["get"].(map[string]any)
	ed, ok := show["externalDocs"].(map[string]any)
	if !ok || ed["url"] != "https://docs.example.com/accounts" || ed["description"] != "Account API guide" {
		t.Errorf("operation externalDocs wrong: %v", show["externalDocs"])
	}
}

func TestParamLevelAttributes(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	get := doc["paths"].(map[string]any)["/accounts"].(map[string]any)["get"].(map[string]any)
	var sort map[string]any
	for _, raw := range get["parameters"].([]any) {
		if p := raw.(map[string]any); p["name"] == "sort" {
			sort = p
		}
	}
	if sort == nil {
		t.Fatal("sort param missing")
	}
	dep, _ := sort["deprecated"].(bool)
	explode, hasExplode := sort["explode"].(bool)
	if !dep || sort["style"] != "form" || !hasExplode || explode {
		t.Errorf("param-level attributes wrong: %v", sort)
	}
}

func TestResponseSummary(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	resp := func(version string) map[string]any {
		doc := api.Emit(gen.EmitOptions{Version: version})
		watch := doc["paths"].(map[string]any)["/accounts/watch"].(map[string]any)["get"].(map[string]any)
		return watch["responses"].(map[string]any)["200"].(map[string]any)
	}
	if resp("3.2.0")["summary"] != "Account change feed" {
		t.Errorf("response summary missing in 3.2: %v", resp("3.2.0"))
	}
	if _, ok := resp("3.1.0")["summary"]; ok {
		t.Error("response summary is 3.2-only, must not appear in 3.1")
	}
}

func TestSchemaExtensionsTag(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	weight := api.Schemas["testdata.Account"].Properties["weight"]
	if nullable, ok := weight.Extensions["x-nullable"].(bool); !ok || !nullable {
		t.Errorf("x-nullable should be true: %v", weight.Extensions)
	}
	if weight.Extensions["x-unit"] != "score" {
		t.Errorf("x-unit should be score: %v", weight.Extensions)
	}
	if indexed, ok := weight.Extensions["x-indexed"].(bool); !ok || indexed {
		t.Errorf("!x-indexed should be false: %v", weight.Extensions)
	}
}

func TestResponseHeaders(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	show := doc["paths"].(map[string]any)["/accounts/{id}"].(map[string]any)["get"].(map[string]any)
	responses := show["responses"].(map[string]any)

	h200 := responses["200"].(map[string]any)["headers"].(map[string]any)
	etag, ok := h200["Etag"].(map[string]any)
	if !ok || etag["description"] != "concurrency control version" {
		t.Errorf("Etag header wrong: %v", h200)
	}
	if etag["schema"].(map[string]any)["type"] != "string" {
		t.Errorf("Etag header schema wrong: %v", etag)
	}
	// `all` attaches to every response.
	if _, ok := h200["X-Request-Id"]; !ok {
		t.Errorf("X-Request-Id missing on 200: %v", h200)
	}
	h404 := responses["404"].(map[string]any)["headers"].(map[string]any)
	if _, ok := h404["X-Request-Id"]; !ok {
		t.Errorf("X-Request-Id missing on 404: %v", h404)
	}
	if _, ok := h404["Etag"]; ok {
		t.Error("Etag should only attach to 200")
	}
}

func TestMapAndSwaggertypeFields(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	acct := api.Schemas["testdata.Account"]

	meta := acct.Properties["meta"]
	if !slices.Contains(meta.Type, "object") || meta.AdditionalProperties == nil ||
		!slices.Contains(meta.AdditionalProperties.Type, "string") {
		t.Errorf("map[string]string should be object+additionalProperties string: %+v", meta)
	}
	attrs := acct.Properties["attrs"]
	if !slices.Contains(attrs.Type, "object") || attrs.AdditionalProperties != nil {
		t.Errorf("map[string]any should be a free-form object: %+v", attrs)
	}
	raw := acct.Properties["raw"]
	if !slices.Contains(raw.Type, "string") || raw.Format != "base64" {
		t.Errorf("swaggertype override should win over []byte: %+v", raw)
	}
}

func TestAdditionalOperations(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}

	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})
	item := doc["paths"].(map[string]any)["/accounts/{id}"].(map[string]any)
	addl, ok := item["additionalOperations"].(map[string]any)
	if !ok {
		t.Fatalf("additionalOperations missing, keys: %v", keysOf(item))
	}
	purge, ok := addl["PURGE"].(map[string]any)
	if !ok {
		t.Fatalf("PURGE not under additionalOperations: %v", keysOf(addl))
	}
	// primitive {string} response kind
	schema := contentSchema(purge["responses"].(map[string]any)["202"])
	if schema["type"] != "string" {
		t.Errorf("{string} response kind should emit a string schema: %v", schema)
	}

	// 3.1 has no additionalOperations; the custom verb downgrades to post.
	item31 := api.Emit(gen.EmitOptions{Version: "3.1.0"})["paths"].(map[string]any)["/accounts/{id}"].(map[string]any)
	if _, ok := item31["additionalOperations"]; ok {
		t.Error("additionalOperations must not appear in 3.1")
	}
	if _, ok := item31["post"]; !ok {
		t.Errorf("custom verb should downgrade to post in 3.1, keys: %v", keysOf(item31))
	}
}

func TestSecurityScopesAndAccessCodeFlow(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})

	// @Security OAuth2Code[read, admin]: scopes in brackets.
	watch := doc["paths"].(map[string]any)["/accounts/watch"].(map[string]any)["get"].(map[string]any)
	scopes := watch["security"].([]any)[0].(map[string]any)["OAuth2Code"].([]any)
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "admin" {
		t.Errorf("security scopes wrong: %v", scopes)
	}

	// swag's accesscode flow maps to authorizationCode with both URLs.
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	flow := schemes["OAuth2Code"].(map[string]any)["flows"].(map[string]any)["authorizationCode"].(map[string]any)
	if flow["authorizationUrl"] != "https://auth.example.com/authorize" ||
		flow["tokenUrl"] != "https://auth.example.com/token" {
		t.Errorf("authorizationCode flow urls wrong: %v", flow)
	}
	if flow["scopes"].(map[string]any)["admin"] != "full access" {
		t.Errorf("flow scopes wrong: %v", flow)
	}
}

func TestDocumentAndTagExternalDocs(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.2.0"})

	ed, ok := doc["externalDocs"].(map[string]any)
	if !ok || ed["url"] != "https://docs.example.com" || ed["description"] != "Platform documentation" {
		t.Errorf("document externalDocs wrong: %v", doc["externalDocs"])
	}

	for _, raw := range doc["tags"].([]any) {
		tag := raw.(map[string]any)
		if tag["name"] != "accounts" {
			continue
		}
		ted, ok := tag["externalDocs"].(map[string]any)
		if !ok || ted["url"] != "https://docs.example.com/tags/accounts" || ted["description"] != "Accounts guide" {
			t.Errorf("tag externalDocs wrong: %v", tag)
		}
		return
	}
	t.Error("accounts tag not found")
}

func TestEmitYAML(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := api.EmitYAML(gen.EmitOptions{Version: "3.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	y := string(data)

	// Version and status codes must be quoted so they stay strings.
	for _, want := range []string{
		"openapi: \"3.2.0\"",
		"\"200\":",
		"\"/accounts/{id}\":",
		"title: \"Example API\"",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("YAML output missing %q\n%s", want, y)
		}
	}
	if strings.Contains(y, "\t") {
		t.Error("YAML must not contain tabs")
	}
}

func TestVersionDowngrade(t *testing.T) {
	api, err := gen.Parse([]string{"../testdata"})
	if err != nil {
		t.Fatal(err)
	}
	doc := api.Emit(gen.EmitOptions{Version: "3.1.0"})
	paths := doc["paths"].(map[string]any)
	search := paths["/accounts/search"].(map[string]any)
	// In 3.1 there is no query method; it downgrades to post.
	if _, ok := search["query"]; ok {
		t.Error("query should not exist in 3.1 output")
	}
	if _, ok := search["post"]; !ok {
		t.Error("query should downgrade to post in 3.1")
	}
}

func keysOf(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func containsAny(s []any, want string) bool {
	for _, v := range s {
		if str, ok := v.(string); ok && strings.EqualFold(str, want) {
			return true
		}
	}
	return false
}
