package settings

import (
	"bytes"
	"errors"
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

func keys(t *testing.T, data []byte, path string) []string {
	t.Helper()
	var out []string
	r := gjson.ParseBytes(data)
	if path != "" {
		r = r.Get(path)
	}
	r.ForEach(func(k, _ gjson.Result) bool {
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
	for _, cmd := range []string{"bash ~/my-cool-statusline.sh", "python /opt/someoneelse/statusline.exe --their-flag", "node /opt/theirs/statusline.js"} {
		in := []byte(`{"statusLine":{"type":"command","command":"` + cmd + `"}}`)
		if _, err := Install(in, ours, false); !errors.Is(err, ErrForeign) {
			t.Fatalf("%q: expected ErrForeign, got %v", cmd, err)
		}
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
	res, err := Install([]byte(fixture), ours, false)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Data
	if k := keys(t, got, ""); strings.Join(k, ",") != "theme,permissions,env,statusLine,enabledPlugins" {
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
	if res.ReplacedLegacy != "node C:/Users/will/.claude/statusline.js" {
		t.Fatalf("the Node installer's entry must be replaced without --force and reported: %+v", res)
	}
	if !strings.Contains(string(got), "    \"allow\": [\n      \"Bash(ls)\",\n      \"Read\"\n    ],") {
		t.Fatalf("arrays must keep one element per line:\n%s", got)
	}
}

func TestInstallIdempotent(t *testing.T) {
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

func TestInstallRejectsInvalidJSON(t *testing.T) {
	if _, err := Install([]byte("{not json"), ours, false); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v", err)
	}
}

func TestUninstall(t *testing.T) {
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
	if k := keys(t, res.Data, ""); strings.Join(k, ",") != "theme,permissions,env,enabledPlugins" {
		t.Fatalf("key order: %v", k)
	}

	foreign := []byte(`{"statusLine":{"type":"command","command":"bash ~/other.sh"}}`)
	res, err = Uninstall(foreign, ours)
	if err != nil || res.Removed || res.Changed || !bytes.Equal(res.Data, foreign) {
		t.Fatalf("foreign statusLine must be untouched: %+v err=%v", res, err)
	}
	res, err = Uninstall(nil, ours)
	if err != nil || res.Removed {
		t.Fatalf("empty file: %+v err=%v", res, err)
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
