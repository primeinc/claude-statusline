// Package render turns one Claude Code status line payload into one terminal line.
//
// Output shape:
//
//	<cwd>/ <model> [ctx] <owner/repo> ⎇ <branch> scratch sess
//
// Every repo-controlled value (path, branch, remote) is stripped of control
// and bidi-override characters before it reaches stdout, and every URL is
// built from percent-encoded segments, so a hostile repository cannot inject
// terminal escape sequences through the status line.
package render

import (
	"encoding/json"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/primeinc/claude-statusline/internal/gitinfo"
)

// Payload is the subset of the status line stdin JSON this program reads.
// Field contract: https://code.claude.com/docs/en/statusline ("Available data").
// Percentages and token counts are pointers because the documented contract
// allows them to be null or absent; a missing field must render as missing.
type Payload struct {
	CWD            string `json:"cwd"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Model          struct {
		ID string `json:"id"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		// Repo is Claude Code's own parse of the origin remote (docs: absent
		// outside a repository or without origin). When present it is the
		// identity source, because git resolves include, insteadOf and BOM
		// cases this program's .git/config reader does not.
		Repo *struct {
			Host  string `json:"host"`
			Owner string `json:"owner"`
			Name  string `json:"name"`
		} `json:"repo"`
	} `json:"workspace"`
	ContextWindow struct {
		TotalInputTokens    *float64 `json:"total_input_tokens"`
		ContextWindowSize   *float64 `json:"context_window_size"`
		UsedPercentage      *float64 `json:"used_percentage"`
		RemainingPercentage *float64 `json:"remaining_percentage"`
	} `json:"context_window"`
}

// ParsePayload returns the zero Payload and ok=false for empty or malformed
// input; the caller renders the visibly degraded line and may report why.
func ParsePayload(r io.Reader) (p Payload, ok bool) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Payload{}, false
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Payload{}, false
	}
	return p, true
}

// Env is everything the renderer reads from the process, injectable for tests.
type Env struct {
	Getenv    func(string) string
	Home      string
	GOOS      string
	DirExists func(string) bool
}

// OSEnv is the real process environment.
func OSEnv() Env {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // no tilde collapse; every other field still renders
	}
	return Env{
		Getenv: os.Getenv,
		Home:   home,
		GOOS:   goos,
		DirExists: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && st.IsDir()
		},
	}
}

// hyperlinks decides whether to emit OSC 8. FORCE_HYPERLINK is Claude Code's
// own override (CHANGELOG 2.1.x: "set FORCE_HYPERLINK=0 to opt out"): "0"
// disables, any other non-empty value enables. Without it, WT_SESSION marks
// Windows Terminal, which supports OSC 8 but is not auto-detected by Claude
// Code (observed on this machine; the installer sets FORCE_HYPERLINK=1 for
// that reason).
func hyperlinks(env Env) bool {
	switch v := env.Getenv("FORCE_HYPERLINK"); v {
	case "":
		return env.Getenv("WT_SESSION") != ""
	case "0":
		return false
	default:
		return true
	}
}

// Render produces the status line, always terminated by one newline.
func Render(p Payload, env Env) string {
	cwd := p.Workspace.CurrentDir
	if cwd == "" {
		cwd = p.CWD
	}
	links := hyperlinks(env)

	var fields []string
	if cwd != "" {
		fields = append(fields, chip(links, fileURL(cwd), displayPath(cwd, env)))
	}
	if slug := modelSlug(p.Model.ID); slug != "" {
		fields = append(fields, slug)
	}
	fields = append(fields, contextField(p))
	if l, ok := repoLinks(p, cwd); ok {
		if links {
			fields = append(fields, chip(true, l.RepoURL, l.Slug))
		} else {
			fields = append(fields, cleanText(l.RepoURL))
		}
		if l.RefLabel != "" {
			fields = append(fields, "⎇ "+chip(links, l.RefURL, l.RefLabel))
		}
	}
	fields = append(fields, sessionChips(p, env, links)...)
	return strings.Join(fields, " ") + "\n"
}

// repoLinks takes repository identity from the payload when Claude Code
// supplies it, otherwise from .git/config; HEAD always comes from .git.
func repoLinks(p Payload, cwd string) (gitinfo.Links, bool) {
	var info gitinfo.Info
	if cwd != "" {
		info, _ = gitinfo.Discover(cwd)
	}
	if r := p.Workspace.Repo; r != nil && r.Host != "" && r.Owner != "" && r.Name != "" {
		return gitinfo.BuildRemote(gitinfo.Remote{Host: strings.ToLower(r.Host), Slug: r.Owner + "/" + r.Name}, info.Head)
	}
	if info.RemoteURL == "" {
		return gitinfo.Links{}, false
	}
	return gitinfo.Build(info)
}

// sessionChips links the two per-session directories Claude Code keeps.
//
//	sess    dirname(transcript_path)/<session_id>/   (subagents/, tool-results/)
//	scratch <tmpdir>/claude/<project-segment>/<session_id>/scratchpad
//
// The project segment is taken from transcript_path rather than recomputed
// from cwd, so Claude Code's path sanitization is never reimplemented here.
// Upstream documents transcript_path and session_id; the scratchpad layout is
// observed, not documented. The session folder is created lazily, so the chip
// falls back to the project folder until it exists. Neither chip renders
// without hyperlinks: their only value is the click target.
func sessionChips(p Payload, env Env, links bool) []string {
	if !links || p.TranscriptPath == "" || p.SessionID == "" {
		return nil
	}
	projectDir := filepath.Dir(p.TranscriptPath)
	var out []string
	scratch := filepath.Join(tempDir(env), "claude", filepath.Base(projectDir), p.SessionID, "scratchpad")
	if env.DirExists(scratch) {
		out = append(out, chip(true, fileURL(scratch), "scratch"))
	}
	sess := filepath.Join(projectDir, p.SessionID)
	if !env.DirExists(sess) {
		sess = projectDir
	}
	out = append(out, chip(true, fileURL(sess), "sess"))
	return out
}

// tempDir mirrors Node's os.tmpdir(), which is what Claude Code used to create
// the scratchpad (nodejs/node lib/os.js tmpdir): on Windows TEMP, then TMP,
// then %SystemRoot%\temp; elsewhere libuv's TMPDIR, TMP, TEMP, TEMPDIR, then
// /tmp. Go's own os.TempDir checks TMP before TEMP, so it is not used.
func tempDir(env Env) string {
	if env.GOOS == windows {
		for _, k := range []string{"TEMP", "TMP"} {
			if v := env.Getenv(k); v != "" {
				return v
			}
		}
		root := env.Getenv("SystemRoot")
		if root == "" {
			root = env.Getenv("windir")
		}
		return filepath.Join(root, "temp")
	}
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "TEMPDIR"} {
		if v := env.Getenv(k); v != "" {
			return v
		}
	}
	return "/tmp"
}

// displayPath collapses the home prefix to "~", normalizes separators to "/",
// and appends a trailing "/" to mark a directory.
func displayPath(p string, env Env) string {
	s := filepath.ToSlash(p)
	h := strings.TrimSuffix(filepath.ToSlash(env.Home), "/")
	if h != "" {
		fold := env.GOOS == windows
		switch {
		case equal(s, h, fold):
			s = "~"
		case len(s) > len(h) && s[len(h)] == '/' && equal(s[:len(h)], h, fold):
			s = "~" + s[len(h):]
		}
	}
	if !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

func equal(a, b string, fold bool) bool {
	if fold {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// fileURL builds a file:// URL for a directory with every path segment
// percent-encoded (spaces, #, %, ?, non-ASCII, control bytes). A trailing "/"
// marks the directory (RFC 8089). UNC paths keep the server as URL host.
func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	s := strings.TrimSuffix(filepath.ToSlash(abs), "/")
	prefix := "file:///"
	if strings.HasPrefix(s, "//") {
		prefix = "file://"
		s = strings.TrimPrefix(s, "//")
	}
	parts := strings.Split(strings.TrimPrefix(s, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return prefix + strings.Join(parts, "/") + "/"
}

// modelSlug shortens a model id for display:
//
//	claude-sonnet-4-6-20251001 -> sonnet-4.6
//	claude-opus-4-7[1m]        -> opus-4.7
//	claude-opus-5              -> opus-5
//
// Only a trailing "<major>-<minor>" pair becomes "<major>.<minor>".
func modelSlug(id string) string {
	s := strings.TrimPrefix(id, "claude-")
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '-'); i >= 0 && len(s)-i-1 == 8 && isDigits(s[i+1:]) {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '-'); i >= 0 && isDigits(s[i+1:]) {
		if j := strings.LastIndexByte(s[:i], '-'); j >= 0 && isDigits(s[j+1:i]) {
			s = s[:i] + "." + s[i+1:]
		}
	}
	return s
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// contextField prefers the exact [used/total] form, then a percentage, and
// renders "[ctx —]" when the payload carries none of them. It never fabricates
// a default such as [0/200k].
func contextField(p Payload) string {
	cw := p.ContextWindow
	switch {
	case cw.TotalInputTokens != nil && cw.ContextWindowSize != nil:
		return "[" + fmtTokens(*cw.TotalInputTokens) + "/" + fmtTokens(*cw.ContextWindowSize) + "]"
	case cw.UsedPercentage != nil:
		return "[" + clampPct(*cw.UsedPercentage) + "%]"
	case cw.RemainingPercentage != nil:
		return "[" + clampPct(100-*cw.RemainingPercentage) + "%]"
	}
	return "[ctx —]"
}

// fmtTokens: 12500 -> 13k, 999999 -> 1M, 1200000 -> 1.2M, 999500000 -> 999.5M.
// The M boundary sits at 999500 so rounding never produces "1000k".
func fmtTokens(n float64) string {
	switch {
	case n >= 999_500:
		m := math.Round(n/100_000) / 10
		return strconv.FormatFloat(m, 'f', -1, 64) + "M"
	case n >= 1_000:
		return strconv.Itoa(int(math.Round(n/1_000))) + "k"
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// clampPct rounds and clamps to the documented 0–100 range.
func clampPct(v float64) string {
	return strconv.Itoa(int(math.Round(math.Max(0, math.Min(100, v)))))
}

// cleanText strips C0 and C1 control bytes plus the zero-width and
// bidirectional-override characters that can reorder or hide terminal text:
// U+200B–U+200F, U+2028–U+202E, U+2060–U+2064, U+2066–U+2069, U+FEFF.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r >= 0x7f && r <= 0x9f,
			r >= 0x200b && r <= 0x200f,
			r >= 0x2028 && r <= 0x202e,
			r >= 0x2060 && r <= 0x2064,
			r >= 0x2066 && r <= 0x2069,
			r == 0xfeff:
			return -1
		}
		return r
	}, s)
}

// chip wraps label in an OSC 8 hyperlink (ESC ] 8 ; ; url BEL label ESC ] 8 ; ; BEL)
// when hyperlinks are enabled. Both inputs are sanitized.
func chip(links bool, u, label string) string {
	label = cleanText(label)
	if !links {
		return label
	}
	return "\x1b]8;;" + cleanText(u) + "\a" + label + "\x1b]8;;\a"
}
