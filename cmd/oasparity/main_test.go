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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/indykite/openapi-parser/gen"
)

const sampleSwagger = `basePath: /configs/v1
consumes:
- application/json
definitions:
  httpproxy.errorResponse:
    properties:
      message:
        type: string
    type: object
  httpproxy.listConfigResponse-httpproxy_readApplicationResponse:
    type: object
info:
  title: Config REST API
  version: "1"
paths:
  /applications:
    get:
      description: List Applications in provided Project with optional
        filtering, wrapped onto the next line with deeper indent.
      responses:
        "200":
          description: OK
    parameters:
    - in: query
      name: project_id
    post:
      responses:
        "201":
          description: Created
  /applications/{id}:
    delete:
      responses:
        "204":
          description: No Content
swagger: "2.0"
`

func TestScanSwag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swagger.yaml")
	if err := os.WriteFile(path, []byte(sampleSwagger), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := scanBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if inv.oas3 {
		t.Error("a swagger 2.0 document must not be flagged oas3")
	}

	wantOps := []string{"get /applications", "post /applications", "delete /applications/{id}"}
	if len(inv.ops) != len(wantOps) {
		t.Errorf("want %d ops, got %v", len(wantOps), sortedKeys(inv.ops))
	}
	for _, op := range wantOps {
		if !inv.ops[op] {
			t.Errorf("missing op %q in %v", op, sortedKeys(inv.ops))
		}
	}

	wantDefs := []string{
		"httpproxy.errorResponse",
		"httpproxy.listConfigResponse-httpproxy_readApplicationResponse",
	}
	if len(inv.defs) != len(wantDefs) {
		t.Errorf("want %d defs, got %v", len(wantDefs), sortedKeys(inv.defs))
	}
	for _, def := range wantDefs {
		if !inv.defs[def] {
			t.Errorf("missing def %q in %v", def, sortedKeys(inv.defs))
		}
	}
}

const sampleOpenAPI32 = `components:
  schemas:
    svc.Ping:
      properties:
        ok:
          type: boolean
      type: object
    svc.listResponse-svc_Item:
      type: object
info:
  title: T
  version: "1"
openapi: 3.2.0
paths:
  /ping:
    additionalOperations:
      PURGE:
        responses:
          "202":
            description: accepted
    get:
      responses:
        "200":
          description: pong
  "/ping/{id}":
    delete:
      responses:
        "204":
          description: gone
  /search:
    query:
      responses:
        "200":
          description: hits
webhooks:
  ping.created:
    post:
      responses:
        "204":
          description: ok
`

func TestScanBaselineOAS3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swagger.yaml")
	if err := os.WriteFile(path, []byte(sampleOpenAPI32), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := scanBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.oas3 {
		t.Error("a document with a top-level openapi version must be flagged oas3")
	}
	wantOps := []string{"get /ping", "purge /ping", "delete /ping/{id}", "query /search"}
	if len(inv.ops) != len(wantOps) {
		t.Errorf("want %d ops, got %v", len(wantOps), sortedKeys(inv.ops))
	}
	for _, op := range wantOps {
		if !inv.ops[op] {
			t.Errorf("missing op %q in %v", op, sortedKeys(inv.ops))
		}
	}
	for _, def := range []string{"svc.Ping", "svc.listResponse-svc_Item"} {
		if !inv.defs[def] {
			t.Errorf("missing def %q in %v", def, sortedKeys(inv.defs))
		}
	}
	if len(inv.hooks) != 1 || !inv.hooks["ping.created"] {
		t.Errorf("want webhook ping.created, got %v", sortedKeys(inv.hooks))
	}
}

func TestDiscoverServices(t *testing.T) {
	repo := t.TempDir()
	for _, svc := range []string{
		"services/a/internal/rest",
		"services/b",
		"vendor/thirdparty", // must be skipped
	} {
		dir := filepath.Join(repo, svc, "docs")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "swagger.yaml"), []byte("paths:\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := discoverServices(repo, "docs/swagger.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join("services", "a", "internal", "rest"),
		filepath.Join("services", "b"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("discovered %v, want %v", got, want)
	}
}

const fixtureGo = `package svc

// Ping is the health response.
type Ping struct {
	OK bool ` + "`json:\"ok\"`" + `
}

// @Summary ping
// @Success 200 {object} Ping "pong"
// @Router /ping [get]
func Handler() {}

// @Summary search — a 3.2-only method swag output can never contain
// @Success 200 {object} Ping "hits"
// @Router /search [query]
func Search() {}

// @Summary purge — a custom verb, additionalOperations in a 3.2 baseline
// @Success 202 "accepted"
// @Router /ping [purge]
func Purge() {}

// @Summary delete — '{' makes a 3.2 baseline quote the path key
// @Success 204 "gone"
// @Router /ping/{id} [delete]
func Remove() {}

// @Webhook ping.created
// @Summary webhook — also absent from swag output by definition
// @Success 204 "ok"
func PingCreated() {}
`

const fixtureSwaggerOK = `definitions:
  svc.Ping:
    type: object
info:
  title: T
paths:
  /ping:
    get:
      responses:
        "200":
          description: pong
  /ping/{id}:
    delete:
      responses:
        "204":
          description: gone
swagger: "2.0"
`

const fixtureSwaggerMismatch = `definitions:
  svc.Ping:
    type: object
  svc.Ghost:
    type: object
paths:
  /ping:
    get:
      responses:
        "200":
          description: pong
    post:
      responses:
        "200":
          description: pong
  /ping/{id}:
    delete:
      responses:
        "204":
          description: gone
swagger: "2.0"
`

func writeFixture(t *testing.T, swagger string) string {
	t.Helper()
	repo := t.TempDir()
	svc := filepath.Join(repo, "svc")
	if err := os.MkdirAll(filepath.Join(svc, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "handler.go"), []byte(fixtureGo), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "docs", "swagger.yaml"), []byte(swagger), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestCheckServiceMatch also proves that operations swag cannot represent
// the [query] op and the webhook in fixtureGo are excluded from the
// comparison instead of being reported as extra.
func TestCheckServiceMatch(t *testing.T) {
	repo := writeFixture(t, fixtureSwaggerOK)
	if err := checkService(repo, "svc", "docs/swagger.yaml", true); err != nil {
		t.Fatalf("matching service should pass: %v", err)
	}
}

func TestCheckServiceNestedLayout(t *testing.T) {
	// docs/swagger.yaml at the service root, annotated handlers in a
	// subdirectory — the standard layout; harvesting must be recursive.
	repo := t.TempDir()
	svc := filepath.Join(repo, "svc")
	for _, dir := range []string{filepath.Join(svc, "docs"), filepath.Join(svc, "internal", "rest")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(svc, "internal", "rest", "handler.go"), []byte(fixtureGo), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "docs", "swagger.yaml"), []byte(fixtureSwaggerOK), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkService(repo, "svc", "docs/swagger.yaml", false); err != nil {
		t.Fatalf("nested service layout should pass: %v", err)
	}
}

func TestCheckServiceMismatch(t *testing.T) {
	repo := writeFixture(t, fixtureSwaggerMismatch)
	err := checkService(repo, "svc", "docs/swagger.yaml", false)
	if err == nil {
		t.Fatal("mismatched service should fail")
	}
	msg := err.Error()
	for _, want := range []string{"missing operation: post /ping", "missing schema: svc.Ghost"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q, got:\n%s", want, msg)
		}
	}
}

// writeFixtureOAS3 builds a service from fixtureGo and returns oasgen's own
// 3.2 output as the baseline text (not yet written to disk).
func writeFixtureOAS3(t *testing.T) (repo, baseline string) {
	t.Helper()
	repo = t.TempDir()
	svc := filepath.Join(repo, "svc")
	if err := os.MkdirAll(filepath.Join(svc, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "handler.go"), []byte(fixtureGo), 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := gen.Parse([]string{svc})
	if err != nil {
		t.Fatal(err)
	}
	data, err := api.EmitYAML(gen.EmitOptions{Version: "3.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	return repo, string(data)
}

func writeBaseline(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "svc", "docs", "swagger.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCheckServiceOAS3SelfParity is the regression-gate use case: a service
// checked against oasgen's own 3.2 output must pass, with the query op, the
// custom verb, and the webhook compared instead of skipped.
func TestCheckServiceOAS3SelfParity(t *testing.T) {
	repo, baseline := writeFixtureOAS3(t)
	writeBaseline(t, repo, baseline)
	if err := checkService(repo, "svc", "docs/swagger.yaml", true); err != nil {
		t.Fatalf("self-parity against own 3.2 output should pass: %v", err)
	}
}

func TestCheckServiceOAS3WebhookMismatch(t *testing.T) {
	repo, baseline := writeFixtureOAS3(t)
	// rename the baseline's webhook: ours becomes extra, the baseline's missing
	writeBaseline(t, repo, strings.Replace(baseline, "ping.created", "ping.deleted", 1))
	err := checkService(repo, "svc", "docs/swagger.yaml", false)
	if err == nil {
		t.Fatal("webhook mismatch should fail")
	}
	for _, want := range []string{"missing webhook: ping.deleted", "extra webhook: ping.created"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%s", want, err)
		}
	}
}

func TestCheckServiceMissingDocs(t *testing.T) {
	repo := t.TempDir()
	if err := checkService(repo, "nope", "docs/swagger.yaml", false); err == nil {
		t.Error("missing swagger.yaml should fail")
	}
}

func TestServiceListExplicit(t *testing.T) {
	list, err := serviceList(".", " a, b ,,c ", "docs/swagger.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if len(list) != 3 || list[0] != want[0] || list[1] != want[1] || list[2] != want[2] {
		t.Errorf("explicit list: %v, want %v", list, want)
	}
}

func TestServiceListEmptyDiscovery(t *testing.T) {
	if _, err := serviceList(t.TempDir(), "", "docs/swagger.yaml"); err == nil {
		t.Error("empty discovery should error with guidance")
	}
}

// normalizeName is applied to BOTH sides of the comparison, so any symmetric
// transformation preserves matches including for names with legitimate
// underscores. These cases document why the blanket replacement is safe where
// per-segment heuristics (e.g. "first underscore only") would corrupt
// array_-prefixed generic arguments.
func TestNormalizeName(t *testing.T) {
	cases := []struct{ swag, ours string }{
		// swag turns dots into underscores inside generic argument names
		{
			"httpproxy.listConfigResponse-httpproxy_readApplicationResponse",
			"httpproxy.listConfigResponse-httpproxy.readApplicationResponse",
		},
		// a real underscore in the type name survives symmetrically
		{
			"pkg.page-pkg_read_config_response",
			"pkg.page-pkg.read_config_response",
		},
		// array-prefixed generic argument ([]pkg.Type)
		{
			"pkg.page-array_pkg_Item",
			"pkg.page-array_pkg.Item",
		},
		// plain names with real underscores, identical on both sides
		{"pkg.snake_case_type", "pkg.snake_case_type"},
	}
	for _, c := range cases {
		if normalizeName(c.swag) != normalizeName(c.ours) {
			t.Errorf("should normalize equal:\n  swag %q -> %q\n  ours %q -> %q",
				c.swag, normalizeName(c.swag), c.ours, normalizeName(c.ours))
		}
	}
}
