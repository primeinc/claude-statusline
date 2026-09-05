// Package settings wires the status line into Claude Code's settings.json
// without disturbing anything else in the file: edits are surgical (sjson),
// key order is preserved, and the result is re-indented with two spaces and
// one array element per line, the layout Claude Code itself writes.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

// ErrForeign means settings.json already has a statusLine that this program
// did not write. Install refuses it unless forced.
var ErrForeign = errors.New("settings.json has a statusLine entry from another tool")

// ErrInvalidJSON means settings.json exists but is not a JSON object.
var ErrInvalidJSON = errors.New("settings.json is not a JSON object")

// Result reports what Install or Uninstall changed.
type Result struct {
	Data           []byte // file content after the operation
	Changed        bool   // Data differs from the input
	AddedHyperlink bool   // env.FORCE_HYPERLINK was added
	Removed        bool   // Uninstall removed statusLine
	ReplacedLegacy string // the Node installer's command that Install replaced, "" otherwise
}

// legacyMarker is the header comment of the Node script the previous
// installer copied to ~/.claude/statusline.js. A command string alone cannot
// distinguish that installer's entry from a hand-written one at the same
// path; the file content can.
const legacyMarker = "claude-statusline"

// Injected for tests.
var (
	readFile = os.ReadFile
	homeDir  = os.UserHomeDir
)

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
	prev := existing.Get("command").String()
	legacy := existing.Exists() && IsLegacy(prev)
	foreign := existing.Exists() && !legacy && !isOurs(prev, command)
	if foreign && !force {
		return Result{}, ErrForeign
	}
	keepExtras := existing.Exists() && !foreign && !legacy
	out, err := setStatusLine(in, command, keepExtras)
	if err != nil {
		return Result{}, err
	}
	res := Result{}
	if legacy {
		res.ReplacedLegacy = prev
	}
	if !gjson.GetBytes(out, "env.FORCE_HYPERLINK").Exists() {
		if out, err = sjson.SetBytes(out, "env.FORCE_HYPERLINK", "1"); err != nil {
			return Result{}, fmt.Errorf("editing env.FORCE_HYPERLINK: %w", err)
		}
		res.AddedHyperlink = true
	}
	res.Data = format(out)
	res.Changed = !bytes.Equal(res.Data, current)
	return res, nil
}

// setStatusLine points statusLine at command. With keepExtras (the entry is
// already ours) only type and command are touched, so user additions such as
// padding survive. Otherwise the object is replaced whole, in place, so its
// position in the file does not move.
func setStatusLine(in []byte, command string, keepExtras bool) ([]byte, error) {
	if keepExtras {
		out, err := sjson.SetBytes(in, "statusLine.type", "command")
		if err != nil {
			return nil, fmt.Errorf("editing statusLine.type: %w", err)
		}
		if out, err = sjson.SetBytes(out, "statusLine.command", command); err != nil {
			return nil, fmt.Errorf("editing statusLine.command: %w", err)
		}
		return out, nil
	}
	raw, err := json.Marshal(struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}{"command", command})
	if err != nil {
		return nil, fmt.Errorf("encoding statusLine: %w", err)
	}
	out, err := sjson.SetRawBytes(in, "statusLine", raw)
	if err != nil {
		return nil, fmt.Errorf("editing statusLine: %w", err)
	}
	return out, nil
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
	out, err := sjson.DeleteBytes(in, "statusLine")
	if err != nil {
		return Result{}, fmt.Errorf("editing statusLine: %w", err)
	}
	res := Result{Data: format(out), Removed: true}
	res.Changed = !bytes.Equal(res.Data, current)
	return res, nil
}

// Existing returns the current statusLine entry as JSON text, "" when absent.
func Existing(current []byte) string {
	return gjson.GetBytes(current, "statusLine").Raw
}

// isOurs is an exact comparison of the command strings (slashes and quotes
// normalised). A substring or basename test would claim a wrapper that
// mentions this binary, or an unrelated tool's */claude-statusline.exe.
func isOurs(existingCommand, command string) bool {
	return canon(existingCommand) == canon(command)
}

func canon(c string) string {
	return strings.Trim(filepath.ToSlash(strings.TrimSpace(c)), `"`)
}

// IsLegacy reports whether command is the previous Node installer's entry:
// "node <path>" where <path> is a file carrying that script's header. The
// path shape alone is what the Claude Code docs suggest for any Node script,
// so the file content is the deciding evidence.
func IsLegacy(command string) bool {
	rest, found := strings.CutPrefix(strings.TrimSpace(command), "node ")
	if !found {
		return false
	}
	p := canon(rest)
	if !strings.HasSuffix(p, "/statusline.js") {
		return false
	}
	if after, ok := strings.CutPrefix(p, "~/"); ok {
		home, err := homeDir()
		if err != nil {
			return false
		}
		p = filepath.Join(home, after)
	}
	content, err := readFile(p)
	if err != nil {
		return false
	}
	return bytes.Contains(content, []byte(legacyMarker))
}

func normalize(current []byte) ([]byte, error) {
	if len(bytes.TrimSpace(current)) == 0 {
		return []byte("{}"), nil
	}
	if !gjson.ValidBytes(current) || !gjson.ParseBytes(current).IsObject() {
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
