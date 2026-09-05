package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderNeverFails(t *testing.T) {
	for _, in := range []string{"", "{}", `{"cwd":"D:/x"}`, "garbage", "not json {{", "[]"} {
		var out, errOut bytes.Buffer
		if code := run(nil, strings.NewReader(in), &out, &errOut); code != 0 {
			t.Fatalf("%q: exit %d", in, code)
		}
		if !strings.HasSuffix(out.String(), "\n") || strings.TrimSpace(out.String()) == "" {
			t.Fatalf("%q: output must be one non-empty line, got %q", in, out.String())
		}
	}
	var out, errOut bytes.Buffer
	run(nil, strings.NewReader("not json {{"), &out, &errOut)
	if out.String() != "[ctx —]\n" {
		t.Fatalf("malformed input must degrade visibly, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "rendering what parsed") {
		t.Fatalf("malformed input must be reported on stderr, got %q", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	run(nil, strings.NewReader(`{"cwd":"D:/x","model":{"id":5}}`), &out, &errOut)
	if !strings.Contains(errOut.String(), `field "model.id"`) || !strings.Contains(out.String(), "D:/x/") {
		t.Fatalf("a mistyped field must be named on stderr and the rest rendered: out=%q err=%q", out.String(), errOut.String())
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
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
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
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temp file must not be left behind: %v", entries)
	}
	out.Reset()
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("second install: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"uninstall", "--settings", path}, nil, &out, &errOut); code != 0 {
		t.Fatalf("uninstall exit %d: %s", code, errOut.String())
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"statusLine"`) || !strings.Contains(string(data), `"FORCE_HYPERLINK": "1"`) {
		t.Fatalf("after uninstall:\n%s", data)
	}
	out.Reset()
	if code := run([]string{"uninstall", "--settings", path, "--print"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "nothing to uninstall") {
		t.Fatalf("uninstall --print with nothing installed: exit %d out %q", code, out.String())
	}
}

func TestWriteFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", link}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	st, err := os.Lstat(link)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink must survive the write: %v %v", st, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), `"statusLine"`) {
		t.Fatalf("the link target must receive the content: %q %v", data, err)
	}
}

func TestWriteKeepsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("mode changed by the temp+rename write: %v -> %v", before.Mode().Perm(), after.Mode().Perm())
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

func TestNonObjectSettingsIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"install", "--settings", path}, nil, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, out %q err %q", code, out.String(), errOut.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "[]" {
		t.Fatalf("file must be untouched, got %q", after)
	}
}

func TestFlagsAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "install") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"install", "-h"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "install") {
		t.Fatalf("install -h: %d %q", code, out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("-h must print one help text, on stdout only; stderr got %q", errOut.String())
	}
	if code := run([]string{"bogus"}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("unknown: %d %q", code, errOut.String())
	}
	errOut.Reset()
	path := filepath.Join(t.TempDir(), "settings.json")
	if code := run([]string{"install", "extra", "--settings", path}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unexpected argument") {
		t.Fatalf("stray positional must fail before touching any file: %d %q", code, errOut.String())
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("nothing may be written on a flag error")
	}
	errOut.Reset()
	if code := run([]string{"install", "--bogus"}, nil, &out, &errOut); code != 1 {
		t.Fatalf("unknown flag: %d %q", code, errOut.String())
	}
}
