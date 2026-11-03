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
	allowed int
	reason  string
	owner   string
}

var limits = map[string]int{"file_lines": 500, "function_lines": 80, "complexity": 15, "nesting": 4}

// Exception keys are path|function|metric. Each ceiling has a documented owner and rationale.
var exceptions = map[string]exception{
	"backend/internal/item/itemtest/repo.go|Repo.List|complexity":           {21, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics.", "test-support"},
	"backend/internal/item/itemtest/repo.go|Repo.UndoCompletion|complexity": {19, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics.", "test-support"},
	"backend/internal/item/itemtest/repo.go|matchesCollection|complexity":   {23, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics.", "test-support"},
	"backend/internal/testoidc/provider.go|Provider.authorize|complexity":   {17, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests.", "test-support"},
	"backend/internal/testoidc/provider.go|Provider.idToken|complexity":     {17, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests.", "test-support"},
	"backend/internal/testoidc/provider.go|Provider.token|complexity":       {19, "Test OIDC provider double supplies deterministic authorize, token, and ID-token responses; keep behavior aligned with provider contract tests.", "test-support"},
	"backend/test/oidc-provider/main.go|main|complexity":                    {18, "Standalone test OIDC provider fixture serves deterministic test protocol endpoints; retain its role as test support, not production code.", "test-support"},
	"backend/internal/item/itemtest/repo.go|Repo.Complete|nesting":          {6, "In-memory item repository is a behavioral test double for item service tests; keep parity with tested repository semantics.", "test-support"},
}

var generatedFiles = map[string]bool{
	"internal/identity/httpapi/setup.gen.go":      true,
	"internal/household/httpapi/household.gen.go": true,
	"internal/item/httpapi/item.gen.go":           true,
	"internal/subject/httpapi/subject.gen.go":     true,
	"internal/platform/httpx/health.gen.go":       true,
}

var generatedDirs = map[string]bool{
	"internal/household/postgres/dbgen": true,
	"internal/identity/postgres/dbgen":  true,
	"internal/item/postgres/dbgen":      true,
	"internal/subject/postgres/dbgen":   true,
}

func main() {
	if err := validateExceptions(); err != nil {
		fail(err)
	}
	metrics, err := collect(".")
	if err != nil {
		fail(err)
	}
	if err := check(metrics); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func identity(path, name, kind string) string { return path + "|" + name + "|" + kind }

func validateExceptions() error {
	for id, item := range exceptions {
		parts := strings.Split(id, "|")
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return fmt.Errorf("invalid exception identity %q", id)
		}
		if strings.TrimSpace(item.reason) == "" || strings.TrimSpace(item.owner) == "" {
			return fmt.Errorf("%s exception requires a reason and owner", id)
		}
		limit, ok := limits[parts[2]]
		if !ok || item.allowed <= limit {
			return fmt.Errorf("%s exception allowed=%d must exceed its metric limit", id, item.allowed)
		}
	}
	return nil
}

func collect(root string) (map[string]int, error) {
	values := make(map[string]int)
	add := func(id string, value int) error {
		if _, exists := values[id]; exists {
			return fmt.Errorf("duplicate metric identity: %s", id)
		}
		values[id] = value
		return nil
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			return skipDirectory(rel)
		}
		if !authoredGoFile(path, rel) {
			return nil
		}
		return collectFile(path, rel, add)
	})
	return values, err
}

func skipDirectory(rel string) error {
	if rel != "." && (rel == "testdata" || strings.HasPrefix(rel, "testdata/")) {
		return filepath.SkipDir
	}
	if generatedDirs[rel] {
		return filepath.SkipDir
	}
	return nil
}

func authoredGoFile(path, rel string) bool {
	return filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") && !generatedFiles[rel]
}

func collectFile(path, rel string, add func(string, int) error) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fileLines := 0
	for _, line := range strings.Split(string(text), "\n") {
		if strings.TrimSpace(line) != "" {
			fileLines++
		}
	}
	output := "backend/" + rel
	if err := add(identity(output, "", "file_lines"), fileLines); err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", output, err)
	}
	for _, item := range declarations(fset, output, file) {
		if err := add(identity(output, item.Name, "function_lines"), item.Lines); err != nil {
			return err
		}
		if err := add(identity(output, item.Name, "complexity"), item.Complexity); err != nil {
			return err
		}
		if err := add(identity(output, item.Name, "nesting"), item.Nesting); err != nil {
			return err
		}
	}
	return nil
}

func check(values map[string]int) error {
	var failures []string
	used := make(map[string]bool)
	for id, value := range values {
		parts := strings.Split(id, "|")
		path, name, kind := parts[0], parts[1], parts[2]
		allowed := limits[kind]
		if item, ok := exceptions[id]; ok {
			allowed = item.allowed
			used[id] = true
			if value < allowed {
				failures = append(failures, fmt.Sprintf("%s:%s %s=%d<exception=%d (stale; tighten; %s)", path, name, kind, value, allowed, item.reason))
			}
		}
		if value > allowed {
			failures = append(failures, fmt.Sprintf("%s:%s %s=%d>limit=%d", path, name, kind, value, allowed))
		}
	}
	for id, item := range exceptions {
		if !used[id] {
			failures = append(failures, fmt.Sprintf("%s exception is stale (not measured); %s", id, item.reason))
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
	for _, specification := range declaration.Specs {
		value, ok := specification.(*ast.ValueSpec)
		if !ok {
			continue
		}
		results = append(results, valueLiterals(fset, path, value)...)
	}
	return results
}

func valueLiterals(fset *token.FileSet, path string, value *ast.ValueSpec) []metric {
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
