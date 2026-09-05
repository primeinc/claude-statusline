package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderNeverFails(t *testing.T) {
	for _, in := range []string{"", "{}", `{"cwd":"D:/x"}`, "garbage", "not json {{"} {
		var out, errOut bytes.Buffer
		if code := run(nil, strings.NewReader(in), &out, &errOut); code != 0 {
			t.Fatalf("%q: exit %d", in, code)
		}
		if !strings.HasSuffix(out.String(), "\n") || strings.TrimSpace(out.String()) == "" {
			t.Fatalf("%q: output must be one non-empty line, got %q", in, out.String())
		}
	}
	var out bytes.Buffer
	run(nil, strings.NewReader("not json {{"), &out, &bytes.Buffer{})
	if out.String() != "[ctx —]\n" {
		t.Fatalf("malformed input must degrade visibly, got %q", out.String())
	}
}

func TestInstallRefusesForeignWithExit2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"bash ~/my.sh"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "refusing to overwrite") {
		t.Fatalf("stderr: %q", errOut.String())
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "bash ~/my.sh") {
		t.Fatal("settings must be untouched")
	}
}

func TestInstallThenUninstallRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 0 {
		t.Fatalf("install exit %d: %s", code, errOut.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"FORCE_HYPERLINK": "1"`) || !strings.Contains(string(data), `"statusLine"`) {
		t.Fatalf("written:\n%s", data)
	}
	out.Reset()
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("second install: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"uninstall", "--settings", path}, nil, &out, &errOut); code != 0 {
		t.Fatalf("uninstall exit %d: %s", code, errOut.String())
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), `"statusLine"`) || !strings.Contains(string(data), `"FORCE_HYPERLINK": "1"`) {
		t.Fatalf("after uninstall:\n%s", data)
	}
}

func TestPrintWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", path, "--print"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("--print must not write")
	}
	if !strings.Contains(out.String(), "would write") {
		t.Fatalf("out: %q", out.String())
	}
}

func TestUsageAndUnknown(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "install") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	if code := run([]string{"bogus"}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("unknown: %d %q", code, errOut.String())
	}
}
