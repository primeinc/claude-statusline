package gitinfo

import "testing"

func TestParseRemote(t *testing.T) {
	cases := []struct {
		in     string
		want   Remote
		wantOK bool
	}{
		{"https://example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"https://example.com/owner/repo", Remote{"example.com", "owner/repo"}, true},
		{"http://example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git://example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"ssh://git@example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"ssh://git@example.com:443/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git+ssh://example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git+https://example.com/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git@example.com:owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git@example.com:/owner/repo.git", Remote{"example.com", "owner/repo"}, true},
		{"git@GitHub.com:owner/repo.git", Remote{"github.com", "owner/repo"}, true},
		{"https://github.com/ow ner/repo.git", Remote{"github.com", "ow ner/repo"}, true},
		{"https://github.com/owner/re?po.git", Remote{"github.com", "owner/re"}, true}, // query is not a repo name
		{"example.com/owner/repo", Remote{}, false},
		{"https://example.com/repo", Remote{}, false},
		{"file:///example.com/owner/repo.git", Remote{}, false},
		{"/example.com/owner/repo.git", Remote{}, false},
		{"C:\\example.com\\owner\\repo.git", Remote{}, false},
		{"ssh://git@[/tmp/git-repo", Remote{}, false},
		{"https://github.com/owner/re\x1b]8;;evilpo.git", Remote{}, false}, // net/url rejects control bytes
		{"git@github.com:owner/re\apo.git", Remote{}, false},
		{"", Remote{}, false},
	}
	for _, c := range cases {
		got, ok := ParseRemote(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("%q: got %+v ok=%v, want %+v ok=%v", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
