// Package render turns one Claude Code status line payload into one terminal line.
//
// Output shape:
//
//	<cwd>/ <model> [ctx] <owner/repo> ⎇ <branch> scratch sess
//
// Every repo-controlled value (path, branch, remote) is stripped of C0/C1
// control bytes before it reaches stdout, and every URL is built from
// percent-encoded segments, so a hostile repository cannot inject terminal
// escape sequences through the status line.
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
	} `json:"workspace"`
	ContextWindow struct {
		TotalInputTokens    *float64 `json:"total_input_tokens"`
		ContextWindowSize   *float64 `json:"context_window_size"`
		UsedPercentage      *float64 `json:"used_percentage"`
		RemainingPercentage *float64 `json:"remaining_percentage"`
	} `json:"context_window"`
}

// ParsePayload never fails. Empty or malformed input yields the zero Payload,
// which renders as the visibly degraded line instead of a blank status bar.
func ParsePayload(r io.Reader) Payload {
	var p Payload
	raw, err := io.ReadAll(r)
	if err != nil {
		return p
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Payload{}
	}
	return p
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
	home, _ := os.UserHomeDir()
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

// Render produces the status line, always terminated by one newline.
func Render(p Payload, env Env) string {
	cwd := p.Workspace.CurrentDir
	if cwd == "" {
		cwd = p.CWD
	}
	links := env.Getenv("FORCE_HYPERLINK") != "" || env.Getenv("WT_SESSION") != ""

	var fields []string
	if cwd != "" {
		fields = append(fields, chip(links, fileURL(cwd), displayPath(cwd, env)))
	}
	if slug := modelSlug(p.Model.ID); slug != "" {
		fields = append(fields, slug)
	}
	fields = append(fields, contextField(p))
	if cwd != "" {
		if info, ok := gitinfo.Discover(cwd); ok {
			if l, ok := gitinfo.Build(info); ok {
				if links {
					fields = append(fields, chip(true, l.RepoURL, l.Slug))
				} else {
					fields = append(fields, cleanText(l.RepoURL))
				}
				if l.RefLabel != "" {
					fields = append(fields, "⎇ "+chip(links, l.RefURL, l.RefLabel))
				}
			}
		}
	}
	fields = append(fields, sessionChips(p, env, links)...)
	return strings.Join(fields, " ") + "\n"
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

// tempDir mirrors Node's os.tmpdir() precedence, which is what Claude Code
// used to create the scratchpad: TEMP then TMP on Windows, TMPDIR then TMP
// then TEMP elsewhere. Go's own os.TempDir checks TMP before TEMP.
func tempDir(env Env) string {
	keys := []string{"TMPDIR", "TMP", "TEMP"}
	if env.GOOS == windows {
		keys = []string{"TEMP", "TMP"}
	}
	for _, k := range keys {
		if v := env.Getenv(k); v != "" {
			return v
		}
	}
	if env.GOOS == windows {
		return os.TempDir()
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
// marks the directory (RFC 8089).
func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	s := strings.TrimSuffix(filepath.ToSlash(abs), "/")
	parts := strings.Split(strings.TrimPrefix(s, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "file:///" + strings.Join(parts, "/") + "/"
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

// fmtTokens: 12500 -> 13k, 999999 -> 1M, 1200000 -> 1.2M. The M boundary sits
// at 999500 so rounding never produces "1000k".
func fmtTokens(n float64) string {
	switch {
	case n >= 999_500:
		return strconv.FormatFloat(n/1_000_000, 'g', 3, 64) + "M"
	case n >= 1_000:
		return strconv.Itoa(int(math.Round(n/1_000))) + "k"
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// clampPct rounds and clamps to the documented 0–100 range.
func clampPct(v float64) string {
	return strconv.Itoa(int(math.Round(math.Max(0, math.Min(100, v)))))
}

// cleanText strips C0 and C1 control bytes.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
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
