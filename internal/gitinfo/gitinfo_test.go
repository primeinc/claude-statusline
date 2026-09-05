package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRepo writes a .git directory by hand: enough for every case except
// linked worktrees, which need real git.
func fakeRepo(t *testing.T, remoteURL, head string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\tbare = false\n"
	if remoteURL != "" {
		cfg += "[remote \"origin\"]\n\turl = " + remoteURL + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	write(t, filepath.Join(dir, ".git", "config"), cfg)
	write(t, filepath.Join(dir, ".git", "HEAD"), head)
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverWalksUp(t *testing.T) {
	dir := fakeRepo(t, "https://github.com/owner/repo.git", "ref: refs/heads/main\n")
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	info, ok := Discover(nested)
	if !ok || info.RemoteURL != "https://github.com/owner/repo.git" || info.Head.Branch != "main" {
		t.Fatalf("got ok=%v info=%+v", ok, info)
	}
	if _, ok := Discover(t.TempDir()); ok {
		t.Fatal("a directory without .git must not be a repository")
	}
}

func TestReadHead(t *testing.T) {
	cases := map[string]Head{
		"ref: refs/heads/main\n":                     {Branch: "main"},
		"ref: refs/heads/feature/fix#1\n":            {Branch: "feature/fix#1"},
		"ref: refs/tags/v1\n":                        {},
		"0123456789abcdef0123456789abcdef01234567\n": {SHA: "0123456789abcdef0123456789abcdef01234567"},
		"garbage\n": {},
		"":          {},
	}
	for in, want := range cases {
		p := filepath.Join(t.TempDir(), "HEAD")
		write(t, p, in)
		if got := readHead(p); got != want {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
	if got := readHead(filepath.Join(t.TempDir(), "missing")); got != (Head{}) {
		t.Errorf("missing HEAD: got %+v", got)
	}
}

func TestOriginURL(t *testing.T) {
	cases := map[string]string{
		"[remote \"origin\"]\n\turl = https://github.com/o/r.git\n":                           "https://github.com/o/r.git",
		"[remote \"origin\"]\n\turl = \"https://github.com/owner/re#po.git\"\n":               "https://github.com/owner/re#po.git",
		"[remote \"upstream\"]\n\turl = https://github.com/o/r.git\n":                         "",
		"[core]\n\tworktree = C:\\Users\\x\n[remote \"origin\"]\n\turl = https://g/o/r.git\n": "", // invalid escape: git rejects it too
		"": "",
	}
	for in, want := range cases {
		p := filepath.Join(t.TempDir(), "config")
		write(t, p, in)
		if got := originURL(p); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if got := originURL(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Errorf("missing config: got %q", got)
	}
}

func TestBuildLinks(t *testing.T) {
	cases := []struct {
		name      string
		remote    string
		head      string
		wantOK    bool
		wantRepo  string
		wantSlug  string
		wantLabel string
		wantRef   string
	}{
		{"github https", "https://github.com/owner/repo.git", "ref: refs/heads/main\n", true,
			"https://github.com/owner/repo", "owner/repo", "main", "https://github.com/owner/repo/tree/main"},
		{"github ssh normalised", "git@github.com:owner/repo.git", "ref: refs/heads/main\n", true,
			"https://github.com/owner/repo", "owner/repo", "main", "https://github.com/owner/repo/tree/main"},
		{"github branch with slash and hash", "https://github.com/owner/repo.git", "ref: refs/heads/feature/fix#1\n", true,
			"https://github.com/owner/repo", "owner/repo", "feature/fix#1", "https://github.com/owner/repo/tree/feature/fix%231"},
		{"github detached", "https://github.com/owner/repo.git", "0123456789abcdef0123456789abcdef01234567\n", true,
			"https://github.com/owner/repo", "owner/repo", "HEAD @0123456", "https://github.com/owner/repo/commit/0123456"},
		{"forgejo ssh", "git@git.title.dev:title-dev/az-skills.git", "ref: refs/heads/main\n", true,
			"https://git.title.dev/title-dev/az-skills", "title-dev/az-skills", "main", "https://git.title.dev/title-dev/az-skills/src/branch/main"},
		{"forgejo https", "https://git.title.dev/title-dev/vendorhub.git", "ref: refs/heads/feat/x\n", true,
			"https://git.title.dev/title-dev/vendorhub", "title-dev/vendorhub", "feat/x", "https://git.title.dev/title-dev/vendorhub/src/branch/feat/x"},
		{"forgejo detached", "https://git.title.dev/title-dev/vendorhub.git", "0123456789abcdef0123456789abcdef01234567\n", true,
			"https://git.title.dev/title-dev/vendorhub", "title-dev/vendorhub", "HEAD @0123456", "https://git.title.dev/title-dev/vendorhub/commit/0123456"},
		{"space in remote is encoded", "https://github.com/ow ner/repo.git", "ref: refs/heads/main\n", true,
			"https://github.com/ow%20ner/repo", "ow ner/repo", "main", "https://github.com/ow%20ner/repo/tree/main"},
		{"unreadable HEAD keeps repo chip", "https://github.com/owner/repo.git", "garbage\n", true,
			"https://github.com/owner/repo", "owner/repo", "", ""},
		{"unknown host", "https://gitlab.com/owner/repo.git", "ref: refs/heads/main\n", false, "", "", "", ""},
		{"no remote", "", "ref: refs/heads/main\n", false, "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info, ok := Discover(fakeRepo(t, c.remote, c.head))
			if !ok {
				t.Fatal("fixture not discovered")
			}
			l, ok := Build(info)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v (%+v)", ok, c.wantOK, l)
			}
			if !ok {
				return
			}
			got := Links{Slug: l.Slug, RepoURL: l.RepoURL, RefLabel: l.RefLabel, RefURL: l.RefURL}
			want := Links{Slug: c.wantSlug, RepoURL: c.wantRepo, RefLabel: c.wantLabel, RefURL: c.wantRef}
			if got != want {
				t.Fatalf("got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestLinkedWorktreeResolvesConfigViaCommondir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	main := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("commit", "--allow-empty", "-qm", "init")
	git("remote", "add", "origin", "https://github.com/owner/repo.git")
	wt := filepath.Join(t.TempDir(), "wt")
	git("worktree", "add", wt, "-b", "wt-branch")

	info, ok := Discover(wt)
	if !ok {
		t.Fatal("worktree not discovered")
	}
	if !strings.HasSuffix(filepath.ToSlash(info.GitDir), "/.git/worktrees/wt") {
		t.Fatalf("GitDir must be the per-worktree dir, got %q", info.GitDir)
	}
	// git writes commondir with its own path casing; compare identity, not text.
	want, err := os.Stat(filepath.Join(main, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(info.CommonDir)
	if err != nil || !os.SameFile(want, got) {
		t.Fatalf("CommonDir must be the main .git, got %q (%v)", info.CommonDir, err)
	}
	l, ok := Build(info)
	if !ok || l.RepoURL != "https://github.com/owner/repo" || l.RefURL != "https://github.com/owner/repo/tree/wt-branch" {
		t.Fatalf("got ok=%v %+v", ok, l)
	}
}
