package buildinfo

import "testing"

func TestInjectedSourceCommitWins(t *testing.T) {
	previous := SourceCommit
	t.Cleanup(func() { SourceCommit = previous })

	SourceCommit = "D7F670ED7A179A8003077D806EDEF7E7318C5066"
	if got := Commit(); got != "d7f670ed7a179a8003077d806edef7e7318c5066" {
		t.Fatalf("Commit() = %q", got)
	}
}

func TestInvalidInjectedCommitIsIgnored(t *testing.T) {
	if got := normalizeCommit("not-a-commit"); got != "" {
		t.Fatalf("normalizeCommit() = %q", got)
	}
}
