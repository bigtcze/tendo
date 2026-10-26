package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestMetricsCountControlFlowAndReceiver(t *testing.T) {
	const source = `package sample
func (s *Service) Run(x int) {
 if x > 0 {
  for i := 0; i < x; i++ {
   if i > 1 && i < 4 || i == 9 { continue }
  }
 } else if x < 0 {
  rangeMe := []int{1}
  for range rangeMe { break }
 }
 switch x {
 case 1, 2:
 case 3:
 default:
 }
 switch any(x).(type) { case int: case string: }
 ch := make(chan int)
 select { case <-ch: case ch <- 1: default: }
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "item.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	got := measure(fset, "backend/item.go", "Service.Run", fn)
	if got.Name != "Service.Run" {
		t.Fatalf("name = %q", got.Name)
	}
	// ifs=3, fors=2, switch cases=2, type cases=2, select comms=2,
	// boolean operators=2, plus the base path.
	if got.Complexity != 14 {
		t.Fatalf("complexity = %d, want 14", got.Complexity)
	}
	if got.Nesting != 3 {
		t.Fatalf("nesting = %d, want 3", got.Nesting)
	}
	if got.Lines != 18 {
		t.Fatalf("lines = %d, want 18", got.Lines)
	}
}

func TestNestingCountsSwitchBodiesAndElseBlocks(t *testing.T) {
	const source = `package sample
func F(value any, ch chan int) {
 switch value { case int: if true {} }
 switch value.(type) { case int: if true {} }
 select { case <-ch: if true {} }
 if value == nil {} else { if value != nil {} }
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "nested.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := measure(fset, "nested.go", "F", file.Decls[0].(*ast.FuncDecl))
	if got.Nesting != 2 {
		t.Fatalf("nesting = %d, want 2", got.Nesting)
	}
}

func TestElseIfDoesNotIncreaseNesting(t *testing.T) {
	const source = `package sample
func F(x int) {
 if x == 1 { } else if x == 2 { } else if x == 3 { }
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "chain.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := measure(fset, "chain.go", "F", file.Decls[0].(*ast.FuncDecl))
	if got.Nesting != 1 {
		t.Fatalf("nesting = %d, want 1", got.Nesting)
	}
}

func TestCollectExcludesGeneratedTestsAndDirectories(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{
		"a.go":                      "package p\nfunc A() {}\n",
		"a_test.go":                 "package p\nfunc TestA() {}\n",
		"a.gen.go":                  "package p\nfunc Generated() {}\n",
		"dbgen/generated.go":        "package p\nfunc GeneratedDB() {}\n",
		"testdata/sample/sample.go": "package p\nfunc Fixture() {}\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := collect([]string{root}, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "backend/a.go" {
		t.Fatalf("metrics = %#v", got)
	}
}
