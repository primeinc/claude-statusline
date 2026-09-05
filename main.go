// claude-statusline renders one status line for Claude Code from the JSON
// payload on stdin, and installs itself into ~/.claude/settings.json.
//
//	claude-statusline              render (stdin -> stdout)
//	claude-statusline install      point settings.json statusLine at this binary
//	claude-statusline uninstall    remove it again
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/primeinc/claude-statusline/internal/render"
	"github.com/primeinc/claude-statusline/internal/settings"
)

const (
	exitOK      = 0
	exitError   = 1
	exitRefused = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return renderCmd(stdin, stdout)
	}
	switch args[0] {
	case "install":
		return installCmd(args[1:], stdout, stderr)
	case "uninstall":
		return uninstallCmd(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
	usage(stderr)
	return exitError
}

func usage(w io.Writer) {
	fmt.Fprint(w, `claude-statusline

  claude-statusline                render the status line from stdin JSON
  claude-statusline install        point settings.json statusLine at this binary
  claude-statusline uninstall      remove that entry

install / uninstall flags:
  --settings PATH   settings.json location (env CLAUDE_SETTINGS; default ~/.claude/settings.json)
  --print           show the resulting file, write nothing
install only:
  --force           replace a statusLine written by another tool

exit codes: 0 ok, 1 error, 2 refused to overwrite a foreign statusLine
`)
}

// renderCmd never exits non-zero and never prints nothing: Claude Code blanks
// the status line on either.
func renderCmd(stdin io.Reader, stdout io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(stdout, "[ctx —]")
			code = exitOK
		}
	}()
	fmt.Fprint(stdout, render.Render(render.ParsePayload(stdin), render.OSEnv()))
	return exitOK
}

type settingsFlags struct {
	path  string
	print bool
	force bool
}

func parseSettingsFlags(name string, args []string, allowForce bool, stderr io.Writer) (settingsFlags, bool) {
	var f settingsFlags
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.path, "settings", "", "settings.json path")
	fs.BoolVar(&f.print, "print", false, "dry run")
	if allowForce {
		fs.BoolVar(&f.force, "force", false, "replace a foreign statusLine")
	}
	if err := fs.Parse(args); err != nil {
		return f, false
	}
	if f.path == "" {
		f.path = os.Getenv("CLAUDE_SETTINGS")
	}
	if f.path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "cannot resolve home directory: %v\n", err)
			return f, false
		}
		f.path = filepath.Join(home, ".claude", "settings.json")
	}
	return f, true
}

func selfCommand(stderr io.Writer) (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "cannot resolve own executable path: %v\n", err)
		return "", false
	}
	return settings.Command(exe), true
}

func readSettings(path string, stderr io.Writer) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "cannot read %s: %v\n", path, err)
		return nil, false
	}
	return data, true
}

func writeSettings(path string, data []byte, stderr io.Writer) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "cannot create %s: %v\n", filepath.Dir(path), err)
		return false
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "cannot write %s: %v\n", path, err)
		return false
	}
	return true
}

func installCmd(args []string, stdout, stderr io.Writer) int {
	f, ok := parseSettingsFlags("install", args, true, stderr)
	if !ok {
		return exitError
	}
	command, ok := selfCommand(stderr)
	if !ok {
		return exitError
	}
	current, ok := readSettings(f.path, stderr)
	if !ok {
		return exitError
	}
	res, err := settings.Install(current, command, f.force)
	switch {
	case errors.Is(err, settings.ErrForeign):
		fmt.Fprintf(stderr, "refusing to overwrite an existing statusLine in %s\n  current: %s\n  ours:    %s\nRe-run with --force to replace it. No files were modified.\n",
			f.path, settings.Existing(current), command)
		return exitRefused
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", f.path, err)
		return exitError
	}
	if f.print {
		fmt.Fprintf(stdout, "settings: %s\ncommand:  %s\n--- would write ---\n%s", f.path, command, res.Data)
		return exitOK
	}
	if res.Changed && !writeSettings(f.path, res.Data, stderr) {
		return exitError
	}
	fmt.Fprintf(stdout, "installed claude-statusline\n  command  %s\n", command)
	if res.Changed {
		fmt.Fprintf(stdout, "  wrote    %s\n", f.path)
	} else {
		fmt.Fprintf(stdout, "  settings already up to date: %s\n", f.path)
	}
	if res.AddedHyperlink {
		fmt.Fprintln(stdout, "  added    env.FORCE_HYPERLINK=1 (Windows Terminal needs it for clickable links)")
	}
	if res.ReplacedLegacy != "" {
		fmt.Fprintf(stdout, "  replaced %s (the script file itself is no longer used and can be deleted)\n", res.ReplacedLegacy)
	}
	fmt.Fprintln(stdout, "Claude Code reloads settings on save; restart it if the line does not appear.")
	return exitOK
}

func uninstallCmd(args []string, stdout, stderr io.Writer) int {
	f, ok := parseSettingsFlags("uninstall", args, false, stderr)
	if !ok {
		return exitError
	}
	command, ok := selfCommand(stderr)
	if !ok {
		return exitError
	}
	current, ok := readSettings(f.path, stderr)
	if !ok {
		return exitError
	}
	res, err := settings.Uninstall(current, command)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", f.path, err)
		return exitError
	}
	if f.print {
		fmt.Fprintf(stdout, "settings: %s\n--- would write ---\n%s", f.path, res.Data)
		return exitOK
	}
	if !res.Removed {
		fmt.Fprintf(stdout, "nothing to uninstall: no claude-statusline entry in %s\n", f.path)
		return exitOK
	}
	if res.Changed && !writeSettings(f.path, res.Data, stderr) {
		return exitError
	}
	fmt.Fprintf(stdout, "uninstalled claude-statusline\n  removed statusLine from %s\n", f.path)
	return exitOK
}
