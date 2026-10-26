// Command funcmetrics reports deterministic size and control-flow metrics for Go functions.
package main

import (
	"encoding/json"
	"flag"
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

type metric struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Lines      int    `json:"lines"`
	Complexity int    `json:"complexity"`
	Nesting    int    `json:"nesting"`
}

func main() {
	prefix := flag.String("prefix", "backend", "path prefix for output relative to repository root")
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	metrics, err := collect(roots, *prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metrics); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collect(roots []string, prefix string) ([]metric, error) {
	var results []metric
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && excludedPath(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == "" || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") || excludedPath(path) || filepath.Ext(path) != ".go" {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			outputPath := filepath.ToSlash(filepath.Join(prefix, rel))
			for _, declaration := range file.Decls {
				fn, ok := declaration.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					name = receiverName(fn.Recv.List[0].Type) + "." + name
				}
				results = append(results, measure(fset, outputPath, name, fn))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func excludedPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == "dbgen" || segment == "testdata" {
			return true
		}
	}
	return false
}

func receiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.Ident:
		return value.Name
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return "unknown"
	}
}

func measure(fset *token.FileSet, path, name string, fn *ast.FuncDecl) metric {
	start := fset.Position(fn.Pos()).Line
	end := fset.Position(fn.End()).Line
	result := metric{Path: path, Name: name, Lines: end - start + 1, Complexity: 1}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if _, nestedFunction := node.(*ast.FuncLit); nestedFunction {
			return false
		}
		switch value := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			result.Complexity++
		case *ast.CaseClause:
			if value.List != nil {
				result.Complexity++
			}
		case *ast.CommClause:
			if value.Comm != nil {
				result.Complexity++
			}
		case *ast.BinaryExpr:
			if value.Op == token.LAND || value.Op == token.LOR {
				result.Complexity++
			}
		}
		return true
	})
	result.Nesting = maxNesting(fn.Body.List, 0)
	return result
}

// Else-if is a continuation of the same conditional level, not another nested level.
func maxNesting(statements []ast.Stmt, depth int) int {
	maximum := depth
	for _, statement := range statements {
		if conditional, ok := statement.(*ast.IfStmt); ok {
			if value := maxNesting(conditional.Body.List, depth+1); value > maximum {
				maximum = value
			}
			if value := maxElseNesting(conditional.Else, depth); value > maximum {
				maximum = value
			}
			if depth+1 > maximum {
				maximum = depth + 1
			}
			continue
		}
		nested, counted := childBlocks(statement)
		nextDepth := depth
		if counted {
			nextDepth++
		}
		if nextDepth > maximum {
			maximum = nextDepth
		}
		if value := maxNesting(nested, nextDepth); value > maximum {
			maximum = value
		}
	}
	return maximum
}

func maxElseNesting(statement ast.Stmt, depth int) int {
	if statement == nil {
		return depth
	}
	if nextIf, ok := statement.(*ast.IfStmt); ok {
		return maxNesting([]ast.Stmt{nextIf}, depth)
	}
	return maxNesting([]ast.Stmt{statement}, depth)
}

func childBlocks(statement ast.Stmt) ([]ast.Stmt, bool) {
	switch value := statement.(type) {
	case *ast.ForStmt:
		return value.Body.List, true
	case *ast.RangeStmt:
		return value.Body.List, true
	case *ast.SwitchStmt:
		return value.Body.List, true
	case *ast.TypeSwitchStmt:
		return value.Body.List, true
	case *ast.SelectStmt:
		return value.Body.List, true
	case *ast.BlockStmt:
		return value.List, false
	case *ast.CaseClause:
		return value.Body, false
	case *ast.CommClause:
		return value.Body, false
	default:
		return nil, false
	}
}
