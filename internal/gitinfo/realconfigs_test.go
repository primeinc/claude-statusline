package gitinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/gcfg"
)

// TestRealConfigsParse runs the config reader over every .git/config matched
// by CLAUDE_STATUSLINE_CONFIG_GLOB, so the "N/N real configs parse" claim is
// reproducible on a developer machine:
//
//	CLAUDE_STATUSLINE_CONFIG_GLOB='C:/Users/will/dev/*/.git/config' go test ./internal/gitinfo -run RealConfigs -v
func TestRealConfigsParse(t *testing.T) {
	glob := os.Getenv("CLAUDE_STATUSLINE_CONFIG_GLOB")
	if glob == "" {
		t.Skip("CLAUDE_STATUSLINE_CONFIG_GLOB not set")
	}
	matches, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("glob %q matched nothing", glob)
	}
	failed := 0
	for _, m := range matches {
		var cfg gitConfig
		if err := gcfg.FatalOnly(gcfg.ReadFileInto(&cfg, m)); err != nil {
			failed++
			t.Errorf("%s: %v", m, err)
		}
	}
	t.Logf("parsed ok=%d fail=%d of %d configs", len(matches)-failed, failed, len(matches))
}
