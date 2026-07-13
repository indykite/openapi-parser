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

// Command oasparity is a swag-migration regression gate: it parses each
// service's annotations with gen and compares the operation and schema
// inventories against the checked-in swag output (docs/swagger.yaml).
//
//	oasparity -repo path/to/repo                       # auto-discover services
//	oasparity -repo path/to/repo -services svc/a,svc/b # explicit list
//
// Without -services, every directory under -repo containing the swag output
// (the -docs relative path) is checked. A service fails when an operation
// differs in either direction, or when a swag definition has no oasgen
// counterpart. Extra oasgen components are reported but allowed
// (unreferenced components are valid). Exits 1 on any failure so it can
// gate CI.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/indykite/openapi-parser/gen"
)

func main() {
	var (
		repo     = flag.String("repo", ".", "root of the repository holding the services")
		services = flag.String("services", "",
			"comma-separated service dirs relative to -repo (default: auto-discover by -docs)")
		docs    = flag.String("docs", "docs/swagger.yaml", "swag output path relative to each service dir")
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
	swag, err := scanSwag(filepath.Join(dir, docs))
	if err != nil {
		return err
	}
	api, err := gen.Parse([]string{dir})
	if err != nil {
		return err
	}

	var problems []string

	ourOps := map[string]bool{}
	for i := range api.Operations {
		op := &api.Operations[i]
		if op.Webhook != "" {
			continue // swag 2.0 has no webhooks to compare against
		}
		if !httpMethods[op.Method] {
			// 3.2-only methods (query, custom verbs) cannot appear in swag
			// output either; remapping them (e.g. to post) could collide
			// with a real operation on the same path, so skip like webhooks
			continue
		}
		ourOps[op.Method+" "+op.Path] = true
	}
	for _, op := range sortedKeys(swag.ops) {
		if !ourOps[op] {
			problems = append(problems, "  missing operation: "+op)
		}
	}
	for _, op := range sortedKeys(ourOps) {
		if !swag.ops[op] {
			problems = append(problems, "  extra operation: "+op)
		}
	}

	// Schema names: swag turns dots into underscores inside generic argument
	// names (Base-pkg_Type); normalize both sides before comparing.
	ourSchemas := map[string]bool{}
	for name := range api.Schemas {
		ourSchemas[normalizeName(name)] = true
	}
	covered := 0
	for _, def := range sortedKeys(swag.defs) {
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
	_, _ = fmt.Fprintf(os.Stdout, "%s: OK — %d ops, %d/%d swag schemas covered (+%d extra components)\n",
		svc, len(ourOps), covered, len(swag.defs), extra)
	if verbose && extra > 0 {
		swagNorm := map[string]bool{}
		for def := range swag.defs {
			swagNorm[normalizeName(def)] = true
		}
		for _, name := range sortedKeys(ourSchemas) {
			if !swagNorm[name] {
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

// --- swag swagger.yaml inventory scanner -----------------------------------

// swagInventory is what we extract from a swag-generated swagger.yaml:
// "method path" operation keys and definition names.
type swagInventory struct {
	ops  map[string]bool
	defs map[string]bool
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// scanSwag reads the inventory with an indentation scanner instead of a YAML
// parser (keeping the module dependency-free). swag's generated output is
// strictly 2-space-indented with unquoted keys: top-level sections at column
// 0, path/definition keys at 2 spaces, methods at 4. Wrapped scalar content
// is always deeper, so matching exact depths is unambiguous.
func scanSwag(path string) (*swagInventory, error) {
	//nolint:gosec // path comes from the operator's own -repo/-services flags
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	inv := &swagInventory{ops: map[string]bool{}, defs: map[string]bool{}}
	section, currentPath := "", ""
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimRight(line, "\r ")
		if trimmed == "" {
			continue
		}
		indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
		key, isKey := strings.CutSuffix(strings.TrimSpace(trimmed), ":")
		switch {
		case indent == 0:
			if isKey {
				section = key
			}
		case section == "definitions" && indent == 2 && isKey:
			inv.defs[key] = true
		case section == "paths" && indent == 2 && isKey && strings.HasPrefix(key, "/"):
			currentPath = key
		case section == "paths" && indent == 4 && isKey && httpMethods[key] && currentPath != "":
			inv.ops[key+" "+currentPath] = true
		}
	}
	return inv, nil
}
