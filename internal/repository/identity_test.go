package repository

import "testing"

func TestParseCanonicalGitHubRepository(t *testing.T) {
	for _, input := range []string{
		"https://github.com/Owner/Repo",
		"HTTPS://GITHUB.COM/Owner/Repo.git",
	} {
		identity, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if identity.Repository != "owner/repo" || identity.NormalizedURL != "https://github.com/Owner/Repo" {
			t.Fatalf("Parse(%q) = %#v", input, identity)
		}
	}
}

func TestParseRejectsNonContractRepositoryURLs(t *testing.T) {
	for _, input := range []string{
		"http://github.com/o/r", "ssh://github.com/o/r", "https://gitlab.com/o/r",
		"https://user@github.com/o/r", "https://github.com:443/o/r",
		"https://github.com/o/r?x=1", "https://github.com/o/r#x",
		"https://github.com/o/r/extra", "https://github.com/o/r/",
		"https://github.com/o%2fr/r", `https://github.com/o\r`,
		"https://github.com/所有者/r", " https://github.com/o/r", "https://github.com//r",
	} {
		if _, err := Parse(input); err == nil {
			t.Fatalf("Parse(%q) unexpectedly succeeded", input)
		}
	}
}
