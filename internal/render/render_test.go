package render

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func env(vars map[string]string, home string, dirs ...string) Env {
	exists := map[string]bool{}
	for _, d := range dirs {
		exists[filepath.Clean(d)] = true
	}
	return Env{
		Getenv:    func(k string) string { return vars[k] },
		Home:      home,
		GOOS:      runtime.GOOS,
		DirExists: func(p string) bool { return exists[filepath.Clean(p)] },
	}
}

func payload(cwd string) Payload {
	var p Payload
	p.Workspace.CurrentDir = cwd
	return p
}

func TestParsePayload(t *testing.T) {
	for name, in := range map[string]string{"empty": "", "garbage": "not json {{", "object": "{}"} {
		p := ParsePayload(strings.NewReader(in))
		if p != (Payload{}) {
			t.Errorf("%s: expected zero payload, got %+v", name, p)
		}
	}
	p := ParsePayload(strings.NewReader(`{"cwd":"D:/x","session_id":"s","transcript_path":"t","model":{"id":"claude-opus-4-7"},"context_window":{"used_percentage":null,"total_input_tokens":5}}`))
	if p.CWD != "D:/x" || p.SessionID != "s" || p.TranscriptPath != "t" || p.Model.ID != "claude-opus-4-7" {
		t.Fatalf("scalar fields not parsed: %+v", p)
	}
	if p.ContextWindow.UsedPercentage != nil || p.ContextWindow.TotalInputTokens == nil || *p.ContextWindow.TotalInputTokens != 5 {
		t.Fatalf("null must stay nil and numbers must parse: %+v", p.ContextWindow)
	}
}

func TestContextField(t *testing.T) {
	cases := []struct {
		name         string
		tokens, size *float64
		used, remain *float64
		want         string
	}{
		{"nothing", nil, nil, nil, nil, "[ctx —]"},
		{"used pct rounds", nil, nil, new(42.7), new(57.3), "[43%]"},
		{"remaining only derives used", nil, nil, nil, new(90.0), "[10%]"},
		{"tokens k", new(12500.0), new(200000.0), nil, nil, "[13k/200k]"},
		{"tokens M", new(750000.0), new(1000000.0), nil, nil, "[750k/1M]"},
		{"tokens over", new(1200000.0), new(1000000.0), nil, nil, "[1.2M/1M]"},
		{"999999 promotes to 1M", new(999999.0), new(1000000.0), nil, nil, "[1M/1M]"},
		{"zero before first response", new(0.0), new(200000.0), nil, nil, "[0/200k]"},
		{"tokens win over pct", new(50000.0), new(200000.0), new(25.0), nil, "[50k/200k]"},
		{"tokens need both halves", new(50000.0), nil, new(25.0), nil, "[25%]"},
		{"negative clamps", nil, nil, new(-5.0), nil, "[0%]"},
		{"over clamps", nil, nil, new(150.0), nil, "[100%]"},
	}
	for _, c := range cases {
		var p Payload
		p.ContextWindow.TotalInputTokens = c.tokens
		p.ContextWindow.ContextWindowSize = c.size
		p.ContextWindow.UsedPercentage = c.used
		p.ContextWindow.RemainingPercentage = c.remain
		if got := contextField(p); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestModelSlug(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-7":            "opus-4.7",
		"claude-sonnet-4-6-20251001": "sonnet-4.6",
		"claude-opus-4-7[1m]":        "opus-4.7",
		"claude-fable-5-1":           "fable-5.1",
		"claude-opus-5":              "opus-5",
		"claude-haiku-4-5-20251001":  "haiku-4.5",
		"":                           "",
	}
	for in, want := range cases {
		if got := modelSlug(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDisplayPath(t *testing.T) {
	e := env(nil, "C:/Users/will")
	cases := map[string]string{
		"C:/Users/will/somewhere": "~/somewhere/",
		"C:\\Users\\will\\dev\\x": "~/dev/x/",
		"C:/Users/will":           "~/",
		"C:/Users/willow/x":       "C:/Users/willow/x/",
		"D:/elsewhere/project":    "D:/elsewhere/project/",
		"D:/proj/":                "D:/proj/",
	}
	for in, want := range cases {
		if got := displayPath(in, e); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestFileURL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter expectations")
	}
	cases := map[string]string{
		"D:/x":                 "file:///D:/x/",
		"D:\\x\\":              "file:///D:/x/",
		"D:/my repo#1/café":    "file:///D:/my%20repo%231/caf%C3%A9/",
		"D:/x\x07\x1b]8;;evil": "file:///D:/x%07%1B%5D8%3B%3Bevil/",
	}
	for in, want := range cases {
		if got := fileURL(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestRenderHyperlinkGate(t *testing.T) {
	p := payload("D:/x")
	if out := Render(p, env(nil, "")); strings.Contains(out, "\x1b]8") {
		t.Fatalf("no hyperlink env must mean no OSC 8: %q", out)
	}
	for _, v := range []string{"FORCE_HYPERLINK", "WT_SESSION"} {
		out := Render(p, env(map[string]string{v: "1"}, ""))
		if !strings.Contains(out, "\x1b]8;;file:///D:/x/\a") {
			t.Fatalf("%s must enable OSC 8: %q", v, out)
		}
	}
}

func TestRenderStripsControlChars(t *testing.T) {
	evil := payload("D:/x\x07\x1b]8;;evil\x07")
	out := Render(evil, env(map[string]string{"FORCE_HYPERLINK": "1"}, ""))
	if esc, bel := strings.Count(out, "\x1b"), strings.Count(out, "\a"); esc != 2 || bel != 2 {
		t.Fatalf("only our own OSC 8 framing may reach stdout (2 ESC, 2 BEL), got %d/%d: %q", esc, bel, out)
	}
	out = Render(payload("D:/x\x1b[31mRED"), env(nil, ""))
	if strings.Contains(out, "\x1b[31m") {
		t.Fatalf("CSI must be stripped even without hyperlinks: %q", out)
	}
}

func TestRenderNoLeadingSpace(t *testing.T) {
	var p Payload
	p.Model.ID = "claude-opus-4-7"
	if out := Render(p, env(nil, "")); out != "opus-4.7 [ctx —]\n" {
		t.Fatalf("got %q", out)
	}
	if out := Render(Payload{}, env(nil, "")); out != "[ctx —]\n" {
		t.Fatalf("empty payload: got %q", out)
	}
}

func gitDir(t *testing.T, config, head string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".git", "config"), config)
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), head)
	return dir
}

func TestRenderRepoChips(t *testing.T) {
	dir := gitDir(t, "[remote \"origin\"]\n\turl = git@git.title.dev:title-dev/az-skills.git\n", "ref: refs/heads/feat/x\n")
	out := Render(payload(dir), env(map[string]string{"FORCE_HYPERLINK": "1"}, ""))
	for _, want := range []string{
		"\x1b]8;;https://git.title.dev/title-dev/az-skills\atitle-dev/az-skills\x1b]8;;\a",
		" ⎇ \x1b]8;;https://git.title.dev/title-dev/az-skills/src/branch/feat/x\afeat/x\x1b]8;;\a",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	out = Render(payload(dir), env(nil, ""))
	if !strings.Contains(out, " https://git.title.dev/title-dev/az-skills ⎇ feat/x\n") {
		t.Fatalf("without hyperlinks the repo URL and branch name are plain text: %q", out)
	}
}

func TestRenderHostileHead(t *testing.T) {
	dir := gitDir(t, "[remote \"origin\"]\n\turl = https://github.com/owner/repo.git\n", "ref: refs/heads/ev\x1bil\a\n")
	out := Render(payload(dir), env(map[string]string{"FORCE_HYPERLINK": "1"}, ""))
	if stripped := stripOSC8(out); strings.ContainsAny(stripped, "\x1b\a") {
		t.Fatalf("stray ESC/BEL leaked from HEAD: %q", out)
	}
	if !strings.Contains(out, "\aevil\x1b]8;;\a") {
		t.Fatalf("branch chip must render with control bytes removed: %q", out)
	}
}

func TestRenderHostileRemoteURL(t *testing.T) {
	// gcfg rejects control bytes in a value, so the repo chip degrades to
	// absent; either way nothing hostile may reach stdout.
	dir := gitDir(t, "[remote \"origin\"]\n\turl = https://github.com/owner/re\x1b]8;;evilpo.git\n", "ref: refs/heads/main\n")
	out := Render(payload(dir), env(map[string]string{"FORCE_HYPERLINK": "1"}, ""))
	if stripped := stripOSC8(out); strings.ContainsAny(stripped, "\x1b\a") {
		t.Fatalf("stray ESC/BEL leaked from config: %q", out)
	}
}

func TestSessionChips(t *testing.T) {
	var p Payload
	p.SessionID = "sid"
	p.TranscriptPath = "C:/h/.claude/projects/C--proj/sid.jsonl"
	vars := map[string]string{"FORCE_HYPERLINK": "1", "TEMP": "C:/tmp", "TMP": "C:/wrong"}
	scratch := "C:/tmp/claude/C--proj/sid/scratchpad"
	sess := "C:/h/.claude/projects/C--proj/sid"

	out := sessionChips(p, env(vars, "", scratch), true)
	if len(out) != 2 || !strings.Contains(out[0], "scratch") || !strings.Contains(out[1], "sess") {
		t.Fatalf("expected [scratch sess], got %q", out)
	}
	if !strings.Contains(out[0], fileURL(scratch)) {
		t.Fatalf("scratch target: %q", out[0])
	}
	if !strings.Contains(out[1], fileURL("C:/h/.claude/projects/C--proj")) {
		t.Fatalf("sess must fall back to the project folder until the session folder exists: %q", out[1])
	}

	out = sessionChips(p, env(vars, "", scratch, sess), true)
	if !strings.Contains(out[1], fileURL(sess)) {
		t.Fatalf("sess must target the session folder once it exists: %q", out[1])
	}

	if out = sessionChips(p, env(vars, ""), true); len(out) != 1 || !strings.Contains(out[0], "sess") {
		t.Fatalf("scratch chip must be omitted when its directory is absent: %q", out)
	}
	if out = sessionChips(p, env(vars, "", scratch, sess), false); out != nil {
		t.Fatalf("no chips without hyperlinks: %q", out)
	}
	p.SessionID = ""
	if out = sessionChips(p, env(vars, "", scratch, sess), true); out != nil {
		t.Fatalf("no chips without session_id: %q", out)
	}
}

func TestTempDirPrecedence(t *testing.T) {
	e := env(map[string]string{"TEMP": "C:/temp", "TMP": "C:/tmp", "TMPDIR": "/tmpdir"}, "")
	e.GOOS = "windows"
	if got := tempDir(e); got != "C:/temp" {
		t.Fatalf("windows: TEMP must win over TMP (Node os.tmpdir order), got %q", got)
	}
	e.GOOS = "linux"
	if got := tempDir(e); got != "/tmpdir" {
		t.Fatalf("linux: TMPDIR first, got %q", got)
	}
	e = env(nil, "")
	e.GOOS = "linux"
	if got := tempDir(e); got != "/tmp" {
		t.Fatalf("linux fallback, got %q", got)
	}
}

func stripOSC8(s string) string {
	for {
		i := strings.Index(s, "\x1b]8;;")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], '\a')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
