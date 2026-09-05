package gitinfo

import (
	"net/url"
	"strings"
)

// Remote is an origin URL reduced to its forge host and "owner/repo" slug.
type Remote struct {
	Host string
	Slug string
}

// ParseRemote accepts the remote URL forms git writes: https://, ssh://,
// git://, git+ssh://, git+https://, and scp-like "git@host:owner/repo.git".
// ok is false for anything without a host and an owner/repo path.
func ParseRemote(raw string) (r Remote, ok bool) {
	u, err := parseURL(raw)
	if err != nil || u.Hostname() == "" {
		return Remote{}, false
	}
	slug := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if !strings.Contains(slug, "/") {
		return Remote{}, false
	}
	return Remote{Host: strings.ToLower(u.Hostname()), Slug: slug}, true
}

// parseURL normalizes git remote URLs, including scp-like syntax.
// Adapted from github.com/cli/cli git/url.go (ParseURL), revision
// ad2a3383771268dd5584127aa9f0615af8bf46d2, MIT license.
func parseURL(rawURL string) (*url.URL, error) {
	if !isPossibleProtocol(rawURL) &&
		strings.ContainsRune(rawURL, ':') &&
		// not a Windows path
		!strings.ContainsRune(rawURL, '\\') {
		// support scp-like syntax for ssh protocol
		rawURL = "ssh://" + strings.Replace(rawURL, ":", "/", 1)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "git+https":
		u.Scheme = "https"
	case "git+ssh":
		u.Scheme = "ssh"
	}

	if u.Scheme != "ssh" {
		return u, nil
	}

	if strings.HasPrefix(u.Path, "//") {
		u.Path = strings.TrimPrefix(u.Path, "/")
	}

	u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())

	return u, nil
}

func isPossibleProtocol(u string) bool {
	return strings.HasPrefix(u, "ssh:") ||
		strings.HasPrefix(u, "git+ssh:") ||
		strings.HasPrefix(u, "git:") ||
		strings.HasPrefix(u, "http:") ||
		strings.HasPrefix(u, "git+https:") ||
		strings.HasPrefix(u, "https:") ||
		strings.HasPrefix(u, "ftp:") ||
		strings.HasPrefix(u, "ftps:") ||
		strings.HasPrefix(u, "file:")
}
