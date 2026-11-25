package mcptool

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/bigtcze/tendo/backend/internal/"

// sharedToolImports are the Tendo packages any tool package may import besides
// its own module's application package: the MCP helpers, the scope constants
// and the schedule value types. Everything else (persistence, HTTP handlers,
// another module, identity administration) is out of reach, so a tool can only
// call the narrow port its module declares.
var sharedToolImports = map[string]bool{
	modulePath + "identity":         true,
	modulePath + "identity/httpapi": true,
	modulePath + "schedule":         true,
}

// identityUse matches every use of the identity package; only the scope
// constants are allowed.
var identityUse = regexp.MustCompile(`\bidentity\.([A-Za-z_]+)`)

// forbiddenToolAccess matches the SDK fields that carry the raw token, the
// extra token map or the request headers. Tools reach the principal only
// through identityhttp.MCPUserID.
var forbiddenToolAccess = regexp.MustCompile(`\.Extra\b|GetExtra\b|TokenInfo|\.Header\b`)

// TestMCPToolsReadOnlyThePrincipalHelper scans every internal/*/mcptool package
// (non-test sources), so a new tool package is covered without a list change.
func TestMCPToolsReadOnlyThePrincipalHelper(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "..", "internal", "*", "mcptool"))
	if err != nil {
		t.Fatal(err)
	}
	scanned := scanToolSources(t, roots)
	if scanned < 1 {
		t.Fatalf("architecture scan found no tool sources under %v", roots)
	}
}

// scanToolSources reports each forbidden reference and returns how many
// non-test Go files it read.
func scanToolSources(t *testing.T, roots []string) int {
	t.Helper()
	scanned := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			scanned++
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if m := forbiddenToolAccess.Find(body); m != nil {
				t.Errorf("%s references %q; tools must use identityhttp.MCPUserID only", path, m)
			}
			checkToolImports(t, path, body)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return scanned
}

// checkToolImports allows a tool source to import only its own module's
// application package and the shared MCP packages from Tendo, and to use only
// the scope constants of the identity package.
func checkToolImports(t *testing.T, path string, body []byte) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, body, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	own := modulePath + filepath.Base(filepath.Dir(filepath.Dir(path)))
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		if strings.HasPrefix(imported, "github.com/bigtcze/") && imported != own && !sharedToolImports[imported] {
			t.Errorf("%s imports %s; a tool package may import only %s and the shared MCP packages", path, imported, own)
		}
	}
	for _, m := range identityUse.FindAllSubmatch(body, -1) {
		if name := string(m[1]); name != "MCPScopeRead" && name != "MCPScopeWrite" {
			t.Errorf("%s uses identity.%s; tools may use only the identity scope constants", path, name)
		}
	}
}
