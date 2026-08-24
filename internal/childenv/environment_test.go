package childenv

import (
	"strings"
	"testing"
)

func TestCanonicalAndGitEnvironmentsExcludeSecrets(t *testing.T) {
	t.Setenv("PATH", `C:\Tools`)
	t.Setenv("SystemRoot", `C:\Windows`)
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	t.Setenv("USERNAME", "test-user")
	for _, key := range []string{
		"CWAPI_SECRET", "SLACK_BOT_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY",
		"GH_TOKEN", "GITHUB_TOKEN", "GIT_TRACE", "GIT_CURL_VERBOSE", "GH_DEBUG",
	} {
		t.Setenv(key, "must-not-leak")
	}

	for name, entries := range map[string][]string{
		"canonical": Canonical(),
		"git":       Git(`C:\CWapi\gh`),
	} {
		joined := strings.Join(entries, "\n")
		if strings.Contains(joined, "must-not-leak") {
			t.Fatalf("%s environment leaked a secret: %s", name, joined)
		}
		for _, forbidden := range []string{"GH_TOKEN=", "GITHUB_TOKEN=", "GIT_TRACE=", "GH_DEBUG="} {
			if strings.Contains(strings.ToUpper(joined), forbidden) {
				t.Fatalf("%s environment contains %s: %s", name, forbidden, joined)
			}
		}
	}
	git := strings.Join(Git(`C:\CWapi\gh`), "\n")
	for _, expected := range []string{
		"PATHEXT=" + FixedPathExt,
		"COMSPEC=" + `C:\Windows\System32\cmd.exe`,
		"USERNAME=test-user",
		"GIT_TERMINAL_PROMPT=0",
		"GH_CONFIG_DIR=" + `C:\CWapi\gh`,
	} {
		if !strings.Contains(git, expected) {
			t.Fatalf("Git environment missing %q: %s", expected, git)
		}
	}
}
