package mcptool

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return scanned
}
