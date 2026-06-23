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
	if len(api.Operations) != 3 {
		t.Fatalf("want 3 operations, got %d", len(api.Operations))
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
