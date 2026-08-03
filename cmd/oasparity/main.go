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

// Command oasparity is a spec-parity regression gate: it parses each
// service's annotations with gen and compares the operation and schema
// inventories against the checked-in baseline (docs/swagger.yaml). The
// baseline may be swag's Swagger 2.0 output (the migration case) or a
// previously generated OpenAPI 3.x document (the regression case).
//
//	oasparity -repo path/to/repo                       # auto-discover services
//	oasparity -repo path/to/repo -services svc/a,svc/b # explicit list
//
// Without -services, every directory under -repo containing the baseline
// (the -docs relative path) is checked. A "service" is just such a
// directory — any subtree with annotated Go code and its own baseline; a
// single-binary repo is one service at its root. A service fails when an
// operation
// differs in either direction, or when a baseline schema has no oasgen
// counterpart. Extra oasgen components are reported but allowed
// (unreferenced components are valid). Exits 1 on any failure so it can
// gate CI.
//
// Against a 2.0 baseline, constructs swag cannot express (webhooks, the
// query method, additionalOperations verbs) are excluded from the
// comparison. Against a 3.x baseline they are compared too — so 3.x
// baselines should be 3.2 output (3.1 downgrades those methods to post,
// which would diff against the parsed annotations).
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/indykite/openapi-parser/gen"
)

func main() {
	var (
		repo     = flag.String("repo", ".", "root of the repository holding the services")
		services = flag.String("services", "",
			"comma-separated service dirs relative to -repo (default: auto-discover by -docs)")
		docs    = flag.String("docs", "docs/swagger.yaml", "baseline spec path relative to each service dir")
		verbose = flag.Bool("v", false, "list extra oasgen components too")
	)
	flag.Parse()

	list, err := serviceList(*repo, *services, *docs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "oasparity:", err)
		os.Exit(2)
	}

	failed := false
	for _, svc := range list {
		if err := checkService(*repo, svc, *docs, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "%s: FAIL\n%v\n", svc, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// serviceList resolves the services to check: the explicit -services list,
// or every directory under repo that contains the swag output file.
func serviceList(repo, services, docs string) ([]string, error) {
	if services != "" {
		var list []string
		for svc := range strings.SplitSeq(services, ",") {
			if svc = strings.TrimSpace(svc); svc != "" {
				list = append(list, svc)
			}
		}
		return list, nil
	}
	list, err := discoverServices(repo, docs)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no %s found under %s (pass -services explicitly?)", docs, repo)
	}
	return list, nil
}

// discoverServices walks repo and returns every directory (repo-relative)
// containing the swag output at the docs relative path.
func discoverServices(repo, docs string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "vendor", "node_modules":
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, docs)); statErr == nil {
			if rel, relErr := filepath.Rel(repo, path); relErr == nil {
				out = append(out, rel)
			}
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

func checkService(repo, svc, docs string, verbose bool) error {
	dir := filepath.Join(repo, svc)
	base, err := scanBaseline(filepath.Join(dir, docs))
	if err != nil {
		return err
	}
	api, err := gen.Parse([]string{dir})
	if err != nil {
		return err
	}

	var problems []string

	ourOps := map[string]bool{}
	ourHooks := map[string]bool{}
	for i := range api.Operations {
		op := &api.Operations[i]
		if op.Webhook != "" {
			ourHooks[op.Webhook] = true
			continue
		}
		if !base.oas3 && !httpMethods[op.Method] {
			// 3.2-only methods (query, custom verbs) cannot appear in swag
			// 2.0 output; remapping them (e.g. to post) could collide with
			// a real operation on the same path, so skip like webhooks
			continue
		}
		ourOps[strings.ToLower(op.Method)+" "+op.Path] = true
	}
	for _, op := range sortedKeys(base.ops) {
		if !ourOps[op] {
			problems = append(problems, "  missing operation: "+op)
		}
	}
	for _, op := range sortedKeys(ourOps) {
		if !base.ops[op] {
			problems = append(problems, "  extra operation: "+op)
		}
	}
	if base.oas3 { // 2.0 baselines cannot express webhooks; 3.x ones can
		for _, h := range sortedKeys(base.hooks) {
			if !ourHooks[h] {
				problems = append(problems, "  missing webhook: "+h)
			}
		}
		for _, h := range sortedKeys(ourHooks) {
			if !base.hooks[h] {
				problems = append(problems, "  extra webhook: "+h)
			}
		}
	}

	// Schema names: swag turns dots into underscores inside generic argument
	// names (Base-pkg_Type); normalize both sides before comparing.
	ourSchemas := map[string]bool{}
	for name := range api.Schemas {
		ourSchemas[normalizeName(name)] = true
	}
	covered := 0
	for _, def := range sortedKeys(base.defs) {
		if ourSchemas[normalizeName(def)] {
			covered++
		} else {
			problems = append(problems, "  missing schema: "+def)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	extra := len(ourSchemas) - covered
	_, _ = fmt.Fprintf(os.Stdout, "%s: OK — %d ops, %d/%d baseline schemas covered (+%d extra components)\n",
		svc, len(ourOps), covered, len(base.defs), extra)
	if verbose && extra > 0 {
		baseNorm := map[string]bool{}
		for def := range base.defs {
			baseNorm[normalizeName(def)] = true
		}
		for _, name := range sortedKeys(ourSchemas) {
			if !baseNorm[name] {
				_, _ = fmt.Fprintf(os.Stdout, "    extra: %s\n", name)
			}
		}
	}
	return nil
}

// normalizeName folds swag's generic-argument naming (dots become
// underscores: Base-pkg_Type) onto oasgen's (Base-pkg.Type). The blanket
// replacement is intentionally symmetric: it runs on BOTH sides of the
// comparison, so real underscores in type names still match their own
// normalized form. Distinct names could theoretically collide after
// normalization (a false coverage pass), but per-segment heuristics would
// corrupt array_-prefixed arguments and break on '-' in @name overrides.
func normalizeName(s string) string { return strings.ReplaceAll(s, "_", ".") }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// --- baseline spec inventory scanner ----------------------------------------

// baselineInventory is what we extract from a baseline spec: "method path"
// operation keys, schema names (2.0 definitions or 3.x components.schemas),
// webhook names, and which flavor the document is.
type baselineInventory struct {
	ops   map[string]bool
	defs  map[string]bool
	hooks map[string]bool
	oas3  bool
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// scanMethods are the method keys recognized under a path entry: the standard
// verbs plus 3.2's first-class query (never present in 2.0 output, so it is
// safe to accept unconditionally).
var scanMethods = func() map[string]bool {
	m := maps.Clone(httpMethods)
	m["query"] = true
	return m
}()

// scanBaseline reads the inventory with an indentation scanner instead of a
// YAML parser (keeping the module dependency-free). Both generators emit
// strictly 2-space-indented block YAML: top-level sections at column 0,
// path/definition keys at 2 spaces, methods at 4. In the 3.x flavor schema
// names sit under components.schemas at 4, custom verbs under a path's
// additionalOperations at 6, webhook names under webhooks at 2, and keys may
// be JSON-quoted (oasgen quotes any string with YAML indicator characters,
// e.g. paths containing '{'). Wrapped scalar content is always deeper than
// the matched depths, so exact-depth matching stays unambiguous.
func scanBaseline(path string) (*baselineInventory, error) {
	//nolint:gosec // path comes from the operator's own -repo/-services flags
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc := &baselineScanner{inv: &baselineInventory{
		ops: map[string]bool{}, defs: map[string]bool{}, hooks: map[string]bool{},
	}}
	for line := range strings.SplitSeq(string(data), "\n") {
		sc.scanLine(line)
	}
	return sc.inv, nil
}

// baselineScanner carries the position state threaded through scanLine calls.
type baselineScanner struct {
	inv          *baselineInventory
	section      string
	subsection   string
	currentPath  string
	inAdditional bool
}

func (sc *baselineScanner) scanLine(line string) {
	trimmed := strings.TrimRight(line, "\r ")
	if trimmed == "" {
		return
	}
	indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
	key, isKey := strings.CutSuffix(strings.TrimSpace(trimmed), ":")
	if isKey && strings.HasPrefix(key, `"`) {
		if unquoted, err := strconv.Unquote(key); err == nil {
			key = unquoted
		}
	}
	switch {
	case indent == 0:
		sc.section, sc.subsection = "", ""
		if isKey {
			sc.section = key
		} else if name, _, _ := strings.Cut(strings.TrimSpace(trimmed), ":"); name == "openapi" {
			sc.inv.oas3 = true
		}
	case !isKey:
	case sc.section == "paths":
		sc.pathsLine(indent, key)
	case sc.section == "definitions" && indent == 2:
		sc.inv.defs[key] = true
	case sc.section == "components" && indent == 2:
		sc.subsection = key
	case sc.section == "components" && sc.subsection == "schemas" && indent == 4:
		sc.inv.defs[key] = true
	case sc.section == "webhooks" && indent == 2:
		sc.inv.hooks[key] = true
	}
}

func (sc *baselineScanner) pathsLine(indent int, key string) {
	switch {
	case indent == 2 && strings.HasPrefix(key, "/"):
		sc.currentPath = key
		sc.inAdditional = false
	case indent == 4 && sc.currentPath != "":
		sc.inAdditional = key == "additionalOperations"
		if scanMethods[key] {
			sc.inv.ops[key+" "+sc.currentPath] = true
		}
	case indent == 6 && sc.inAdditional && sc.currentPath != "":
		// custom verbs (emitted uppercase, e.g. PURGE)
		sc.inv.ops[strings.ToLower(key)+" "+sc.currentPath] = true
	}
}
