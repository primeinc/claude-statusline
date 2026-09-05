package gitinfo

import (
	"net/url"
	"strings"
)

// forge holds the URL path prefixes a host uses for a branch view and a
// single-commit view. Hosts absent from this table render no repo chip.
type forge struct {
	branch string
	commit string
}

var forges = map[string]forge{
	// GitHub: /tree/<branch>, /commit/<sha>.
	"github.com": {branch: "/tree/", commit: "/commit/"},
	// Forgejo at git.title.dev: /src/branch/<branch> (modules/git/ref.go:205)
	// and /commit/<sha> (routers/web/web.go:1654), Forgejo revision
	// d7471ea487788c5d6f711a61436a45bf47601948.
	"git.title.dev": {branch: "/src/branch/", commit: "/commit/"},
}

// Links is what the status line renders for a repository.
type Links struct {
	Slug     string // owner/repo
	RepoURL  string // https://<host>/<owner>/<repo>
	RefLabel string // branch name, or "HEAD @<sha7>" when detached
	RefURL   string // branch or commit view; "" when HEAD is unreadable
}

// Build maps repository identity to forge URLs. ok is false when the remote
// is missing, unparseable, or on a host this program does not know.
func Build(info Info) (l Links, ok bool) {
	r, ok := ParseRemote(info.RemoteURL)
	if !ok {
		return Links{}, false
	}
	f, ok := forges[r.Host]
	if !ok {
		return Links{}, false
	}
	l.Slug = r.Slug
	l.RepoURL = "https://" + r.Host + "/" + escapeSegments(r.Slug)
	switch {
	case info.Head.Branch != "":
		l.RefLabel = info.Head.Branch
		l.RefURL = l.RepoURL + f.branch + escapeSegments(info.Head.Branch)
	case info.Head.SHA != "":
		short := info.Head.SHA[:7]
		l.RefLabel = "HEAD @" + short
		l.RefURL = l.RepoURL + f.commit + short
	}
	return l, true
}

// escapeSegments percent-encodes each "/"-separated segment while keeping "/"
// as the path separator, the way forges expect branch names like
// feature/fix#1 (-> feature/fix%231).
func escapeSegments(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
