// Package gitinfo reads repository identity straight from .git files: no git
// subprocess. It resolves linked worktrees through the gitdir pointer file and
// commondir so config comes from the common directory while HEAD comes from
// the per-worktree directory.
package gitinfo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/gcfg"
)

// Head is the current checkout: Branch for a symbolic ref under refs/heads,
// SHA (full hex) for a detached HEAD, both empty when HEAD is unreadable.
type Head struct {
	Branch string
	SHA    string
}

// Info is what the status line needs from a repository.
type Info struct {
	GitDir    string // per-worktree directory holding HEAD
	CommonDir string // directory holding config and refs
	RemoteURL string // remote.origin.url as written, "" when absent
	Head      Head
}

// Discover walks up from dir to the nearest .git entry. ok is false when dir
// is not inside a repository.
func Discover(dir string) (info Info, ok bool) {
	gitDir, ok := findGitDir(dir)
	if !ok {
		return Info{}, false
	}
	info.GitDir = gitDir
	info.CommonDir = commonDir(gitDir)
	info.RemoteURL = originURL(filepath.Join(info.CommonDir, "config"))
	info.Head = readHead(filepath.Join(gitDir, "HEAD"))
	return info, true
}

// findGitDir accepts both a .git directory and a .git file containing
// "gitdir: <path>" (linked worktrees, submodules).
func findGitDir(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, ".git")
		if st, err := os.Stat(candidate); err == nil {
			if st.IsDir() {
				return candidate, true
			}
			if raw, err := os.ReadFile(candidate); err == nil {
				if p, found := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:"); found {
					return resolve(dir, strings.TrimSpace(p)), true
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// commonDir returns the directory named by gitDir/commondir, or gitDir itself
// when that file is absent (a normal, non-worktree checkout).
func commonDir(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	return resolve(gitDir, strings.TrimSpace(string(raw)))
}

func resolve(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// gitConfig is the slice of the git-config grammar this package reads. gcfg
// parses the same syntax git does (subsection quoting, value escapes) and,
// wrapped in FatalOnly, ignores every section and key not declared here.
type gitConfig struct {
	Remote map[string]*struct {
		URL string `gcfg:"url"`
	}
}

// originURL returns remote.origin.url, or "" when the config is missing,
// unparseable, or has no origin. Any failure degrades to "no repo chip".
func originURL(configPath string) string {
	var cfg gitConfig
	if err := gcfg.FatalOnly(gcfg.ReadFileInto(&cfg, configPath)); err != nil {
		return ""
	}
	origin, ok := cfg.Remote["origin"]
	if !ok || origin == nil {
		return ""
	}
	return origin.URL
}

// readHead parses "ref: refs/heads/<branch>" or a bare object id.
func readHead(headPath string) Head {
	raw, err := os.ReadFile(headPath)
	if err != nil {
		return Head{}
	}
	s := strings.TrimSpace(string(raw))
	if ref, found := strings.CutPrefix(s, "ref:"); found {
		if branch, found := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/"); found && branch != "" {
			return Head{Branch: branch}
		}
		return Head{}
	}
	if len(s) >= 7 && len(s) <= 64 && isHex(s) {
		return Head{SHA: s}
	}
	return Head{}
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return s != ""
}
