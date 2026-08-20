package codex

import (
	"strings"
	"testing"
)

func TestAppServerEnvironmentIsBounded(t *testing.T) {
	t.Setenv("PATH", `C:\Tools;C:\Program Files\Git\cmd`)
	t.Setenv("SystemRoot", `C:\Windows`)
	t.Setenv("TEMP", `C:\Temp`)
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	t.Setenv("CODEX_API_KEY", "must-not-leak")
	t.Setenv("CWAPI_UNRELATED_SECRET", "must-not-leak")

	entries := appServerEnvironment()
	joined := strings.Join(entries, "\n")
	for _, expected := range []string{
		`PATH=C:\Tools;C:\Program Files\Git\cmd`,
		`SystemRoot=C:\Windows`,
		`TEMP=C:\Temp`,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected bounded environment entry %q in %q", expected, joined)
		}
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CWAPI_UNRELATED_SECRET", "must-not-leak"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("forbidden environment value leaked: %s", forbidden)
		}
	}
}
