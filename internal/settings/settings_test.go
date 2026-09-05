package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const ours = "C:/Users/will/go/bin/claude-statusline.exe"

const fixture = `{
  "theme": "dark",
  "permissions": {
    "allow": [
      "Bash(ls)",
      "Read"
    ],
    "defaultMode": "bypassPermissions"
  },
  "env": {
    "OTEL_TRACES_EXPORTER": "otlp",
    "FORCE_HYPERLINK": "existing-value"
  },
  "statusLine": {
    "type": "command",
    "command": "node C:/Users/will/.claude/statusline.js"
  },
  "enabledPlugins": {
    "foo": true
  }
}
`

// header is how the previous installer's script begins.
const header = "#!/usr/bin/env node\n// claude-statusline — a single-file, non-blocking statusline for Claude Code.\n// Output: ...\n"

// legacyFS makes "node C:/Users/will/.claude/statusline.js" resolve to a file
// with the given content, and everything else to ENOENT.
func legacyFS(t *testing.T, content string) {
	t.Helper()
	prevRead, prevHome := readFile, homeDir
	readFile = func(p string) ([]byte, error) {
		if filepath.ToSlash(p) == "C:/Users/will/.claude/statusline.js" {
			return []byte(content), nil
		}
		return nil, os.ErrNotExist
	}
	homeDir = func() (string, error) { return "C:/Users/will", nil }
	t.Cleanup(func() { readFile, homeDir = prevRead, prevHome })
}

func keys(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	gjson.ParseBytes(data).ForEach(func(k, _ gjson.Result) bool {
		out = append(out, k.String())
		return true
	})
	return out
}

func TestInstallIntoEmpty(t *testing.T) {
	res, err := Install(nil, ours, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"" + ours + "\"\n  },\n  \"env\": {\n    \"FORCE_HYPERLINK\": \"1\"\n  }\n}\n"
	if string(res.Data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", res.Data, want)
	}
	if !res.Changed || !res.AddedHyperlink {
		t.Fatalf("flags: %+v", res)
	}
}

func TestInstallRefusesForeign(t *testing.T) {
	legacyFS(t, header)
	foreign := []string{
		"bash ~/my-cool-statusline.sh",
		"python /opt/someoneelse/statusline.exe --their-flag",
		"node /opt/theirs/statusline.js",                  // docs-recommended shape, no such file
		"node ~/.claude/other.js",                         // under .claude, wrong name
		"bash C:/tools/wrap.sh " + ours + " --their-flag", // mentions our path; not our entry
		ours + " --extra",                                 // our path plus arguments
		"C:/elsewhere/claude-statusline.exe",              // same basename elsewhere
	}
	for _, cmd := range foreign {
		if _, err := Install(withCommand(t, cmd), ours, false); !errors.Is(err, ErrForeign) {
			t.Fatalf("%q: expected ErrForeign, got %v", cmd, err)
		}
	}
	// The tilde form resolves to the same file, which carries the marker.
	res, err := Install(withCommand(t, "node ~/.claude/statusline.js"), ours, false)
	if err != nil || res.ReplacedLegacy != "node ~/.claude/statusline.js" {
		t.Fatalf("tilde legacy entry: err=%v res=%+v", err, res)
	}
}

func withCommand(t *testing.T, cmd string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"statusLine": map[string]any{"type": "command", "command": cmd}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInstallRecognisesOurs(t *testing.T) {
	variants := []string{ours, `"` + ours + `"`, strings.ReplaceAll(ours, "/", `\`), " " + ours + " "}
	if runtime.GOOS == "windows" {
		variants = append(variants, strings.ToUpper(ours)) // NTFS paths are case-insensitive
	}
	for _, cmd := range variants {
		in, err := json.Marshal(map[string]any{"statusLine": map[string]any{"type": "command", "command": cmd, "padding": 2}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := Install(in, ours, false)
		if err != nil {
			t.Fatalf("%q: %v", cmd, err)
		}
		if res.ReplacedLegacy != "" {
			t.Fatalf("%q: not legacy", cmd)
		}
		if gjson.GetBytes(res.Data, "statusLine.padding").Int() != 2 {
			t.Fatalf("%q: user keys on our own entry must survive:\n%s", cmd, res.Data)
		}
	}
}

func TestInstallLegacyNeedsFileContent(t *testing.T) {
	in := []byte(fixture)
	for _, content := range []string{header, strings.ReplaceAll(header, "\n", "\r\n")} {
		legacyFS(t, content)
		res, err := Install(in, ours, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.ReplacedLegacy != "node C:/Users/will/.claude/statusline.js" {
			t.Fatalf("previous installer's entry must be replaced and reported: %+v", res)
		}
		if gjson.GetBytes(res.Data, "statusLine.command").String() != ours {
			t.Fatalf("command not replaced:\n%s", res.Data)
		}
	}

	foreignScripts := []string{
		"// someone else's node statusline at the same path\n",
		"#!/usr/bin/env node\n// mine\n// see the claude-statusline package notes for FORCE_HYPERLINK\n", // mentions us later
		"// claude-statusline — copied header without the shebang\n",
	}
	for _, content := range foreignScripts {
		legacyFS(t, content)
		if _, err := Install(in, ours, false); !errors.Is(err, ErrForeign) {
			t.Fatalf("%q: same path, different script: must be refused, got %v", content, err)
		}
	}
	legacyFS(t, "")
	readFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if _, err := Install(in, ours, false); !errors.Is(err, ErrForeign) {
		t.Fatalf("same path, no file: must be refused, got %v", err)
	}
}

func TestInstallForceReplacesForeign(t *testing.T) {
	in := []byte(`{"statusLine":{"type":"command","command":"bash ~/x.sh","padding":2}}`)
	res, err := Install(in, ours, true)
	if err != nil {
		t.Fatal(err)
	}
	sl := gjson.GetBytes(res.Data, "statusLine")
	if sl.Get("command").String() != ours || sl.Get("padding").Exists() {
		t.Fatalf("foreign entry must be replaced whole: %s", sl.Raw)
	}
	if gjson.GetBytes(res.Data, "statusLineBackup").Exists() {
		t.Fatal("no backup key is written")
	}
}

func TestInstallPreservesEverythingElse(t *testing.T) {
	legacyFS(t, header)
	res, err := Install([]byte(fixture), ours, false)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Data
	if k := keys(t, got); strings.Join(k, ",") != "theme,permissions,env,statusLine,enabledPlugins" {
		t.Fatalf("top-level key order changed: %v", k)
	}
	checks := map[string]string{
		"theme":                    "dark",
		"permissions.defaultMode":  "bypassPermissions",
		"permissions.allow.1":      "Read",
		"env.OTEL_TRACES_EXPORTER": "otlp",
		"env.FORCE_HYPERLINK":      "existing-value",
		"enabledPlugins.foo":       "true",
		"statusLine.type":          "command",
		"statusLine.command":       ours,
	}
	for path, want := range checks {
		if v := gjson.GetBytes(got, path).String(); v != want {
			t.Errorf("%s: got %q want %q", path, v, want)
		}
	}
	if res.AddedHyperlink {
		t.Fatal("existing FORCE_HYPERLINK must not be reported as added")
	}
	if !strings.Contains(string(got), "    \"allow\": [\n      \"Bash(ls)\",\n      \"Read\"\n    ],") {
		t.Fatalf("arrays must keep one element per line:\n%s", got)
	}
}

func TestInstallIdempotent(t *testing.T) {
	legacyFS(t, header)
	first, err := Install([]byte(fixture), ours, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(first.Data, ours, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || !bytes.Equal(second.Data, first.Data) {
		t.Fatalf("second install must be a no-op:\n%s", second.Data)
	}
}

func TestInstallRejectsNonObject(t *testing.T) {
	for _, in := range []string{"{not json", "[]", `"str"`, "42", "null"} {
		if _, err := Install([]byte(in), ours, false); !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("%q: got %v", in, err)
		}
		if _, err := Uninstall([]byte(in), ours); !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("uninstall %q: got %v", in, err)
		}
	}
}

func TestUninstall(t *testing.T) {
	legacyFS(t, header)
	installed, err := Install([]byte(fixture), ours, false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall(installed.Data, ours)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed || !res.Changed || gjson.GetBytes(res.Data, "statusLine").Exists() {
		t.Fatalf("statusLine must be removed: %+v\n%s", res, res.Data)
	}
	if gjson.GetBytes(res.Data, "env.FORCE_HYPERLINK").String() != "existing-value" {
		t.Fatal("env must be left alone")
	}
	if k := keys(t, res.Data); strings.Join(k, ",") != "theme,permissions,env,enabledPlugins" {
		t.Fatalf("key order: %v", k)
	}

	for _, cmd := range []string{"bash ~/other.sh", "node C:/Users/will/.claude/statusline.js", ours + " --x"} {
		foreign := []byte(`{"statusLine":{"type":"command","command":"` + cmd + `"}}`)
		res, err = Uninstall(foreign, ours)
		if err != nil || res.Removed || res.Changed || !bytes.Equal(res.Data, foreign) {
			t.Fatalf("%q: must be untouched: %+v err=%v", cmd, res, err)
		}
	}
	res, err = Uninstall(nil, ours)
	if err != nil || res.Removed {
		t.Fatalf("empty file: %+v err=%v", res, err)
	}
}

func TestUninstallRestoresNodeInstallerBackup(t *testing.T) {
	in := []byte(`{"a":1,"statusLine":{"type":"command","command":"` + ours + `"},"statusLineBackup":{"type":"command","command":"bash ~/original.sh","padding":3},"z":2}`)
	res, err := Uninstall(in, ours)
	if err != nil || !res.Removed {
		t.Fatalf("%+v err=%v", res, err)
	}
	if got := gjson.GetBytes(res.Data, "statusLine.command").String(); got != "bash ~/original.sh" {
		t.Fatalf("backup must be restored into statusLine, got %q\n%s", got, res.Data)
	}
	if gjson.GetBytes(res.Data, "statusLine.padding").Int() != 3 || gjson.GetBytes(res.Data, "statusLineBackup").Exists() {
		t.Fatalf("backup restored whole and consumed:\n%s", res.Data)
	}
	if k := keys(t, res.Data); strings.Join(k, ",") != "a,statusLine,z" {
		t.Fatalf("key order: %v", k)
	}
}

func TestCommand(t *testing.T) {
	if got := Command(`C:\Users\will\go\bin\claude-statusline.exe`); got != ours {
		t.Fatalf("got %q", got)
	}
	if got := Command(`C:\Program Files\x\claude-statusline.exe`); got != `"C:/Program Files/x/claude-statusline.exe"` {
		t.Fatalf("got %q", got)
	}
	in := []byte(`{"statusLine":{"type":"command","command":"\"C:/Program Files/x/claude-statusline.exe\""}}`)
	if _, err := Install(in, `"C:/Program Files/x/claude-statusline.exe"`, false); err != nil {
		t.Fatalf("a quoted command must still be recognised as ours: %v", err)
	}
}
