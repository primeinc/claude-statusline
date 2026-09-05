// Package settings wires the status line into Claude Code's settings.json
// without disturbing anything else in the file: edits are surgical (sjson),
// key order is preserved, and the result is re-indented with two spaces and
// one array element per line, the layout Claude Code itself writes.
package settings

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

// ErrForeign means settings.json already has a statusLine that this program
// did not write. Install refuses it unless forced.
var ErrForeign = errors.New("settings.json has a statusLine entry from another tool")

// ErrInvalidJSON means settings.json exists but does not parse.
var ErrInvalidJSON = errors.New("settings.json is not valid JSON")

// Result reports what Install or Uninstall changed.
type Result struct {
	Data           []byte // file content after the operation
	Changed        bool   // Data differs from the input
	AddedHyperlink bool   // env.FORCE_HYPERLINK was added
	Removed        bool   // Uninstall removed statusLine
	ReplacedLegacy string // the Node installer's command that Install replaced, "" otherwise
}

// Command is the settings.json statusLine.command for an executable path:
// forward slashes (Git Bash consumes backslashes) and double quotes when the
// path contains whitespace.
func Command(exe string) string {
	c := filepath.ToSlash(exe)
	if strings.ContainsAny(c, " \t") {
		return `"` + c + `"`
	}
	return c
}

// Install points statusLine at command and sets env.FORCE_HYPERLINK=1 when the
// key is absent. A foreign statusLine is refused unless force is true, in
// which case it is replaced.
func Install(current []byte, command string, force bool) (Result, error) {
	in, err := normalize(current)
	if err != nil {
		return Result{}, err
	}
	existing := gjson.GetBytes(in, "statusLine")
	foreign := existing.Exists() && !isOurs(existing.Get("command").String(), command)
	if foreign && !force {
		return Result{}, ErrForeign
	}
	out := in
	if foreign {
		out, _ = sjson.DeleteBytes(out, "statusLine")
	}
	out, _ = sjson.SetBytes(out, "statusLine.type", "command")
	out, _ = sjson.SetBytes(out, "statusLine.command", command)
	res := Result{}
	if prev := existing.Get("command").String(); IsLegacy(prev) {
		res.ReplacedLegacy = prev
	}
	if !gjson.GetBytes(out, "env.FORCE_HYPERLINK").Exists() {
		out, _ = sjson.SetBytes(out, "env.FORCE_HYPERLINK", "1")
		res.AddedHyperlink = true
	}
	res.Data = format(out)
	res.Changed = !bytes.Equal(res.Data, current)
	return res, nil
}

// Uninstall removes statusLine when it points at command. env.FORCE_HYPERLINK
// is left alone: Claude Code's own file links use it too.
func Uninstall(current []byte, command string) (Result, error) {
	in, err := normalize(current)
	if err != nil {
		return Result{}, err
	}
	existing := gjson.GetBytes(in, "statusLine")
	if !existing.Exists() || !isOurs(existing.Get("command").String(), command) {
		return Result{Data: current}, nil
	}
	out, _ := sjson.DeleteBytes(in, "statusLine")
	res := Result{Data: format(out), Removed: true}
	res.Changed = !bytes.Equal(res.Data, current)
	return res, nil
}

// Existing returns the current statusLine entry as JSON text, "" when absent.
func Existing(current []byte) string {
	return gjson.GetBytes(current, "statusLine").Raw
}

// isOurs is a path test, never a basename test: an unrelated tool's
// */statusline.exe must stay foreign. The previous installer of this project
// wrote "node <dir>/.claude/statusline.js"; that exact shape is ours too, so
// upgrading does not need --force.
func isOurs(existingCommand, command string) bool {
	existing := filepath.ToSlash(existingCommand)
	if strings.Contains(existing, strings.Trim(filepath.ToSlash(command), `"`)) {
		return true
	}
	return IsLegacy(existingCommand)
}

// IsLegacy reports whether command is the Node installer's own entry.
func IsLegacy(command string) bool {
	c := filepath.ToSlash(command)
	return strings.HasPrefix(c, "node ") && strings.HasSuffix(c, "/.claude/statusline.js")
}

func normalize(current []byte) ([]byte, error) {
	if len(bytes.TrimSpace(current)) == 0 {
		return []byte("{}"), nil
	}
	if !gjson.ValidBytes(current) {
		return nil, ErrInvalidJSON
	}
	return current, nil
}

// format re-indents with two spaces. Width 0 disables pretty's single-line
// array collapsing so arrays keep one element per line.
func format(b []byte) []byte {
	out := pretty.PrettyOptions(b, &pretty.Options{Width: 0, Indent: "  "})
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return out
}
