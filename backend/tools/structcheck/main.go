// Command structcheck rejects new or worsened structural debt in authored Go code.
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

type metric struct {
	Path, Name                 string
	Lines, Complexity, Nesting int
}

type exception struct {
	path, name, metric string
	allowed            int
	reason             string
}

var limits = map[string]int{"file_lines": 500, "function_lines": 80, "complexity": 15, "nesting": 4}

// Exceptions are reviewed debt, with rationale and an explicit ceiling.
var exceptions = map[string]exception{
	"backend/internal/item/itemtest/repo.go|Repo.List|complexity":           {"backend/internal/item/itemtest/repo.go", "Repo.List", "complexity", 21, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics."},
	"backend/internal/item/itemtest/repo.go|Repo.UndoCompletion|complexity": {"backend/internal/item/itemtest/repo.go", "Repo.UndoCompletion", "complexity", 19, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics."},
	"backend/internal/item/itemtest/repo.go|matchesCollection|complexity":   {"backend/internal/item/itemtest/repo.go", "matchesCollection", "complexity", 23, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics."},
	"backend/internal/testoidc/provider.go|Provider.authorize|complexity":   {"backend/internal/testoidc/provider.go", "Provider.authorize", "complexity", 17, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests."},
	"backend/internal/testoidc/provider.go|Provider.idToken|complexity":     {"backend/internal/testoidc/provider.go", "Provider.idToken", "complexity", 17, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests."},
	"backend/internal/testoidc/provider.go|Provider.token|complexity":       {"backend/internal/testoidc/provider.go", "Provider.token", "complexity", 19, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests."},
	"backend/test/oidc-provider/main.go|main|complexity":                    {"backend/test/oidc-provider/main.go", "main", "complexity", 18, "Standalone test OIDC provider fixture serves deterministic test protocol endpoints; retain its role as test support, not production code."},
	"backend/internal/item/itemtest/repo.go|Repo.Complete|nesting":          {"backend/internal/item/itemtest/repo.go", "Repo.Complete", "nesting", 6, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics."},
}

func main() {
	metrics, err := collect(".")
	if err != nil {
		fail(err)
	}
	if err := check(metrics); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func collect(root string) (map[string]int, error) {
	values := make(map[string]int)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "dbgen" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fileLines := 0
		for _, line := range strings.Split(string(text), "\n") {
			if strings.TrimSpace(line) != "" {
				fileLines++
			}
		}
		values[key("backend/"+rel, "", "file_lines")] = fileLines
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, item := range declarations(fset, strings.TrimPrefix(rel, "./"), file) {
			values[key("backend/"+strings.TrimPrefix(rel, "./"), item.Name, "function_lines")] = item.Lines
			values[key("backend/"+strings.TrimPrefix(rel, "./"), item.Name, "complexity")] = item.Complexity
			values[key("backend/"+strings.TrimPrefix(rel, "./"), item.Name, "nesting")] = item.Nesting
		}
		return nil
	})
	return values, err
}

func key(path, name, metric string) string { return path + "|" + name + "|" + metric }

func check(values map[string]int) error {
	var failures []string
	used := make(map[string]bool)
	for id, value := range values {
		parts := strings.Split(id, "|")
		path, name, kind := parts[0], parts[1], parts[2]
		allowed := limits[kind]
		for _, item := range exceptions {
			if item.path == path && item.name == name && item.metric == kind {
				allowed = item.allowed
				used[id] = true
				if value < allowed {
					failures = append(failures, fmt.Sprintf("%s:%s %s=%d<exception=%d (stale; tighten; %s)", path, name, kind, value, allowed, item.reason))
				}
			}
		}
		if value > allowed {
			failures = append(failures, fmt.Sprintf("%s:%s %s=%d>limit=%d", path, name, kind, value, allowed))
		}
	}
	for _, item := range exceptions {
		id := key(item.path, item.name, item.metric)
		if !used[id] {
			failures = append(failures, item.path+":"+item.name+" "+item.metric+" exception is stale (not measured); "+item.reason)
		}
	}
	sort.Strings(failures)
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n"))
	}
	return nil
}

func declarations(fset *token.FileSet, path string, file *ast.File) []metric {
	var results []metric
	initOrdinal := 0
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "init" {
			initOrdinal++
			results = append(results, functionMetricsNamed(fset, path, function, fmt.Sprintf("init#%d", initOrdinal))...)
			continue
		}
		results = append(results, declarationMetrics(fset, path, declaration)...)
	}
	return results
}

func declarationMetrics(fset *token.FileSet, path string, declaration ast.Decl) []metric {
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		return functionMetrics(fset, path, value)
	case *ast.GenDecl:
		return packageLiteralMetrics(fset, path, value)
	default:
		return nil
	}
}

func functionMetrics(fset *token.FileSet, path string, function *ast.FuncDecl) []metric {
	name := function.Name.Name
	if function.Recv != nil && len(function.Recv.List) > 0 {
		name = receiverName(function.Recv.List[0].Type) + "." + name
	}
	return functionMetricsNamed(fset, path, function, name)
}

func functionMetricsNamed(fset *token.FileSet, path string, function *ast.FuncDecl, name string) []metric {
	if function.Body == nil {
		return nil
	}
	results := []metric{measure(fset, path, name, function)}
	return append(results, literalMetrics(fset, path, name, function.Body)...)
}

func packageLiteralMetrics(fset *token.FileSet, path string, declaration *ast.GenDecl) []metric {
	var results []metric
	for specIndex, specification := range declaration.Specs {
		value, ok := specification.(*ast.ValueSpec)
		if !ok {
			continue
		}
		results = append(results, valueLiterals(fset, path, value, specIndex+1)...)
	}
	return results
}

func valueLiterals(fset *token.FileSet, path string, value *ast.ValueSpec, specOrdinal int) []metric {
	var results []metric
	blankOrdinal := 0
	for index, expression := range value.Values {
		name := "var"
		if index < len(value.Names) {
			name = value.Names[index].Name
			if name == "_" {
				blankOrdinal++
				name = fmt.Sprintf("_#%d.%d", fset.Position(value.Pos()).Offset, blankOrdinal)
			}
		}
		results = append(results, literalsInNode(fset, path, name, expression)...)
	}
	return results
}

func literalsInNode(fset *token.FileSet, path, parent string, node ast.Node) []metric {
	var results []metric
	ordinal := 0
	ast.Inspect(node, func(current ast.Node) bool {
		literal, ok := current.(*ast.FuncLit)
		if !ok {
			return true
		}
		ordinal++
		name := fmt.Sprintf("%s.func%d", parent, ordinal)
		results = append(results, measureLiteral(fset, path, name, literal))
		results = append(results, literalMetrics(fset, path, name, literal.Body)...)
		return false
	})
	return results
}

func literalMetrics(fset *token.FileSet, path, parent string, body *ast.BlockStmt) []metric {
	var results []metric
	ordinal := 0
	ast.Inspect(body, func(node ast.Node) bool {
		if node == body {
			return true
		}
		literal, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		ordinal++
		name := fmt.Sprintf("%s.func%d", parent, ordinal)
		results = append(results, measureLiteral(fset, path, name, literal))
		results = append(results, literalMetrics(fset, path, name, literal.Body)...)
		return false
	})
	return results
}

func excludedPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "testdata" {
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
	return measureBody(fset, path, name, fn.Pos(), fn.End(), fn.Body)
}

func measureLiteral(fset *token.FileSet, path, name string, fn *ast.FuncLit) metric {
	return measureBody(fset, path, name, fn.Pos(), fn.End(), fn.Body)
}

func measureBody(fset *token.FileSet, path, name string, startPos, endPos token.Pos, body *ast.BlockStmt) metric {
	result := metric{Path: path, Name: name, Lines: fset.Position(endPos).Line - fset.Position(startPos).Line + 1, Complexity: 1}
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
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
	result.Nesting = maxNesting(body.List, 0)
	return result
}

// Else-if branches continue the same nesting level; a braced else is nested.
func maxNesting(statements []ast.Stmt, depth int) int {
	maximum := depth
	for _, statement := range statements {
		if value := nestingForStatement(statement, depth); value > maximum {
			maximum = value
		}
	}
	return maximum
}

func nestingForStatement(statement ast.Stmt, depth int) int {
	conditional, ok := statement.(*ast.IfStmt)
	if ok {
		return max(depth+1, maxNesting(conditional.Body.List, depth+1), maxElseNesting(conditional.Else, depth))
	}
	children, counted := childBlocks(statement)
	next := depth
	if counted {
		next++
	}
	return max(next, maxNesting(children, next))
}

func maxElseNesting(statement ast.Stmt, depth int) int {
	if statement == nil {
		return depth
	}
	if _, ok := statement.(*ast.IfStmt); ok {
		return nestingForStatement(statement, depth)
	}
	return maxNesting([]ast.Stmt{statement}, depth)
}

func childBlocks(statement ast.Stmt) ([]ast.Stmt, bool) {
	if labeled, ok := statement.(*ast.LabeledStmt); ok {
		return []ast.Stmt{labeled.Stmt}, false
	}
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

func sortMetrics(items []metric) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].Name < items[j].Name
	})
}
