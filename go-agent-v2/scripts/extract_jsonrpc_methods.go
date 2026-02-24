//go:build ignore

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type rootData struct {
	files  []*ast.File
	consts map[string]string
}

func main() {
	roots := []string{
		"internal/apiserver",
		"internal/dashrpc",
		"internal/skills",
	}
	fset := token.NewFileSet()
	data := map[string]*rootData{}

	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		rd := &rootData{consts: map[string]string{}}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil
			}
			rd.files = append(rd.files, file)
			collectStringConsts(file, rd.consts)
			return nil
		})
		if err != nil {
			continue
		}
		data[root] = rd
	}

	methods := map[string]struct{}{}
	for _, root := range roots {
		rd := data[root]
		if rd == nil {
			continue
		}
		for _, file := range rd.files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.IndexExpr:
					if name, ok := methodFromMethodsIndex(x, rd.consts); ok && validMethod(name) {
						methods[name] = struct{}{}
					}
				case *ast.CallExpr:
					if isRegisterCall(x) && len(x.Args) > 0 {
						if name, ok := methodFromExpr(x.Args[0], rd.consts); ok && validMethod(name) {
							methods[name] = struct{}{}
						}
					}
				}
				return true
			})
		}
	}

	out := make([]string, 0, len(methods))
	for m := range methods {
		out = append(out, m)
	}
	sort.Strings(out)
	for _, m := range out {
		fmt.Println(m)
	}
}

func collectStringConsts(file *ast.File, out map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if s, ok := methodFromExpr(vs.Values[i], nil); ok {
					out[name.Name] = s
				}
			}
		}
	}
}

func methodFromMethodsIndex(idx *ast.IndexExpr, consts map[string]string) (string, bool) {
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "methods" {
		return "", false
	}
	root, ok := sel.X.(*ast.Ident)
	if !ok || root.Name != "s" {
		return "", false
	}
	return methodFromExpr(idx.Index, consts)
}

func isRegisterCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return strings.EqualFold(fn.Name, "register")
	case *ast.SelectorExpr:
		return fn.Sel != nil && strings.EqualFold(fn.Sel.Name, "register")
	default:
		return false
	}
}

func methodFromExpr(expr ast.Expr, consts map[string]string) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		return strings.Trim(v.Value, `"`), true
	case *ast.Ident:
		if consts == nil {
			return "", false
		}
		s, ok := consts[v.Name]
		return s, ok
	default:
		return "", false
	}
}

func validMethod(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			strings.ContainsRune("._/-", r) {
			continue
		}
		return false
	}
	return true
}
