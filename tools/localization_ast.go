//go:build ignore

// Export source strings without executing the application or any agent/tool.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Literal struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"`
	Text    string `json:"text"`
}
type Variable struct {
	File string `json:"file"`
	Name string `json:"name"`
	Text string `json:"text"`
}
type Export struct {
	Literals  []Literal  `json:"literals"`
	Variables []Variable `json:"variables"`
}

func run(input, output string) error {
	sources := map[string]string{}
	info, err := os.Stat(input)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &sources); err != nil {
			return err
		}
	} else {
		err = filepath.WalkDir(input, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", ".next", "data", "tools", "localization":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(input, path)
			if err != nil {
				return err
			}
			sources[filepath.ToSlash(rel)] = strings.TrimPrefix(string(data), "\ufeff")
			return nil
		})
		if err != nil {
			return err
		}
	}
	files := make([]string, 0, len(sources))
	for file := range sources {
		files = append(files, file)
	}
	sort.Strings(files)
	result := Export{Literals: []Literal{}, Variables: []Variable{}}
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, strings.TrimPrefix(sources[file], "\ufeff"), 0)
		if err != nil {
			return err
		}
		var stack []string
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			context := ""
			if len(stack) > 0 {
				context = stack[len(stack)-1]
			}
			switch v := n.(type) {
			case *ast.FuncDecl:
				context = "func:" + v.Name.Name
			case *ast.ValueSpec:
				if len(v.Names) > 0 {
					context = "var:" + v.Names[0].Name
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					text, err := strconv.Unquote(v.Value)
					if err == nil {
						result.Literals = append(result.Literals, Literal{file, fset.Position(v.Pos()).Line, context, text})
					}
				}
			}
			stack = append(stack, context)
			return true
		})
		// Resolve literal concatenations (notably ReporterDefaultPrompt, whose
		// Markdown backticks require several Go literals) without running code.
		exprs := map[string]ast.Expr{}
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range g.Specs {
				v, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range v.Names {
					if i < len(v.Values) {
						exprs[name.Name] = v.Values[i]
					}
				}
			}
		}
		var fold func(ast.Expr, map[string]bool) (string, bool)
		fold = func(expr ast.Expr, seen map[string]bool) (string, bool) {
			switch e := expr.(type) {
			case *ast.BasicLit:
				if e.Kind != token.STRING {
					return "", false
				}
				x, err := strconv.Unquote(e.Value)
				return x, err == nil
			case *ast.BinaryExpr:
				if e.Op != token.ADD {
					return "", false
				}
				a, ok := fold(e.X, seen)
				if !ok {
					return "", false
				}
				b, ok := fold(e.Y, seen)
				return a + b, ok
			case *ast.ParenExpr:
				return fold(e.X, seen)
			case *ast.Ident:
				if seen[e.Name] {
					return "", false
				}
				value, ok := exprs[e.Name]
				if !ok {
					return "", false
				}
				seen[e.Name] = true
				text, ok := fold(value, seen)
				delete(seen, e.Name)
				return text, ok
			}
			return "", false
		}
		names := make([]string, 0, len(exprs))
		for name := range exprs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if text, ok := fold(exprs[name], map[string]bool{}); ok {
				result.Variables = append(result.Variables, Variable{file, name, text})
			}
		}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(data, '\n'), 0644)
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run tools/localization_ast.go <source-directory|source-snapshot.json> <output.json>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
