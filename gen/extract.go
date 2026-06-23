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

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// source holds everything extracted from the Go files before interpretation.
//
// Type resolution is now module-aware: structs are indexed by import path, and
// each file's imports are recorded so a qualified reference like `model.Account`
// can be resolved to the right package — including across packages in the same
// module. (Cross-MODULE / third-party deps are a documented TODO; see resolve.)
type source struct {
	structsByPath map[string]map[string]*structDef
	pkgNameByPath map[string]string
	importsByFile map[string]map[string]string
	pathByFile    map[string]string
	flat          map[string]*structDef
	commentGroups []commentGroup
}

type commentGroup struct {
	text string
	file string
}

type structDef struct {
	name       string
	doc        string
	pkgName    string
	importPath string
	file       string
	fields     []*ast.Field
}

func newSource() *source {
	return &source{
		structsByPath: map[string]map[string]*structDef{},
		pkgNameByPath: map[string]string{},
		importsByFile: map[string]map[string]string{},
		pathByFile:    map[string]string{},
		flat:          map[string]*structDef{},
	}
}

// extract indexes types module-wide and harvests annotations from parseDirs.
func extract(parseDirs []string) (*source, error) {
	src := newSource()
	fset := token.NewFileSet()

	// Normalize the set of dirs we harvest annotations from.
	harvest := map[string]bool{}
	for _, d := range parseDirs {
		if abs, err := filepath.Abs(d); err == nil {
			harvest[abs] = true
		}
	}

	// Find the module root + path so we can index the whole module for types.
	modRoot, modPath := findModuleRoot(firstOr(parseDirs, "."))

	indexFile := func(path string, f *ast.File) {
		importPath := importPathFor(path, modRoot, modPath)
		src.pathByFile[path] = importPath
		if importPath != "" {
			src.pkgNameByPath[importPath] = f.Name.Name
		}
		collectImports(path, f, src)
		collectTypes(path, importPath, f.Name.Name, f, src)
		// Harvest annotations only from the requested dirs.
		if harvest[filepath.Dir(path)] {
			for _, cg := range f.Comments {
				src.commentGroups = append(src.commentGroups, commentGroup{text: cg.Text(), file: path})
			}
		}
	}

	if modRoot != "" {
		// Walk the whole module so cross-package types resolve.
		if err := walkModule(modRoot, fset, indexFile); err != nil {
			return nil, err
		}
		return src, nil
	}

	// No module root: index + harvest only the requested dirs (non-recursive).
	for _, dir := range parseDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return nil, perr
			}
			indexFile(path, f)
		}
	}
	return src, nil
}

// walkModule walks the module tree (skipping vendor and _test.go files) and
// feeds every parsed Go file to index, so cross-package types resolve.
func walkModule(root string, fset *token.FileSet, index func(string, *ast.File)) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir // TODO: optional vendor indexing
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return perr
		}
		index(path, f)
		return nil
	})
}

func collectImports(path string, f *ast.File, src *source) {
	m := map[string]string{}
	for _, imp := range f.Imports {
		ipath := strings.Trim(imp.Path.Value, `"`)
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			m[imp.Name.Name] = ipath // explicit alias
		}
		// Default (no alias): resolved later via findByPkgName, keyed on the
		// imported package's real name.
	}
	src.importsByFile[path] = m
}

func collectTypes(path, importPath, pkgName string, f *ast.File, src *source) {
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			return true
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			doc := ""
			if ts.Doc != nil {
				doc = ts.Doc.Text()
			} else if gd.Doc != nil {
				doc = gd.Doc.Text()
			}
			def := &structDef{
				name:       ts.Name.Name,
				doc:        doc,
				fields:     st.Fields.List,
				pkgName:    pkgName,
				importPath: importPath,
				file:       path,
			}
			if src.structsByPath[importPath] == nil {
				src.structsByPath[importPath] = map[string]*structDef{}
			}
			src.structsByPath[importPath][ts.Name.Name] = def
			if _, exists := src.flat[ts.Name.Name]; !exists {
				src.flat[ts.Name.Name] = def
			}
		}
		return true
	})
}

// --- module discovery ----------------------------------------------------

func findModuleRoot(start string) (root, modulePath string) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", ""
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		//nolint:gosec // gomod is discovered by walking up the tree, not attacker-controlled
		if data, err := os.ReadFile(gomod); err == nil {
			return dir, parseModulePath(string(data))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "" // reached filesystem root
		}
		dir = parent
	}
}

func parseModulePath(gomod string) string {
	for line := range strings.SplitSeq(gomod, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// importPathFor maps a file path to its package import path within the module.
func importPathFor(file, modRoot, modPath string) string {
	if modRoot == "" || modPath == "" {
		return ""
	}
	rel, err := filepath.Rel(modRoot, filepath.Dir(file))
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return modPath
	}
	return modPath + "/" + rel
}

func firstOr(ss []string, def string) string {
	if len(ss) > 0 {
		return ss[0]
	}
	return def
}

// --- group classification (unchanged) ------------------------------------

var generalMarkers = map[string]bool{
	"title": true, "version": true, "host": true, "basepath": true,
	"termsofservice": true, "schemes": true, "externaldocs.url": true,
}

func isGeneralGroup(ds []directive) bool {
	for _, d := range ds {
		if generalMarkers[d.name] || strings.HasPrefix(d.name, "contact.") ||
			strings.HasPrefix(d.name, "license.") || strings.HasPrefix(d.name, "securitydefinitions.") ||
			strings.HasPrefix(d.name, "tag.") {
			return true
		}
	}
	return false
}

func isOperationGroup(ds []directive) bool {
	for _, d := range ds {
		if d.name == "router" {
			return true
		}
	}
	return false
}
