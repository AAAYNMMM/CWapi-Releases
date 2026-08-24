//go:build windows

package invocation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
)

func TestResolverUsesCanonicalPathAndConfinesCWD(t *testing.T) {
	systemRoot := os.Getenv("SystemRoot")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	work := filepath.Join(root, "work")
	sub := filepath.Join(work, "sub")
	for _, directory := range []string{bin, sub} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(bin, "sample.exe")
	if err := os.WriteFile(executable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("SystemRoot", systemRoot)
	resolver, err := New(childenv.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(root, "changed"))
	final, err := resolver.Resolve(work, processcontract.StartArguments{Command: "sample", CWD: "sub", Argv: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(final.Executable, executable) || !strings.EqualFold(final.CWD, sub) || final.Argv[0] != "a" {
		t.Fatalf("final=%#v", final)
	}
	for _, input := range []processcontract.StartArguments{
		{Command: "sample", CWD: "../outside"},
		{Command: "C:sample.exe"},
		{Command: "sample.ps1"},
	} {
		if _, err := resolver.Resolve(work, input); err == nil {
			t.Fatalf("unsafe invocation accepted: %#v", input)
		}
	}
}

func TestBatchBridgePreservesMetacharacterArguments(t *testing.T) {
	systemRoot := os.Getenv("SystemRoot")
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(systemRoot, "System32"))
	t.Setenv("SystemRoot", systemRoot)
	resolver, err := New(childenv.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target with spaces.cmd")
	output := filepath.Join(root, "arguments.txt")
	script := "@echo off\r\nsetlocal DisableDelayedExpansion\r\nset \"CWOUT=%~1\"\r\nset \"CWTEST01=[%~2]\"\r\nset \"CWTEST02=[%~3]\"\r\nset \"CWTEST03=[%~4]\"\r\nset \"CWTEST04=[%~5]\"\r\nset \"CWTEST05=[%~6]\"\r\nset \"CWTEST06=[%~7]\"\r\nset \"CWTEST07=[%~8]\"\r\nset \"CWTEST08=[%~9]\"\r\nshift\r\nset \"CWTEST09=[%~9]\"\r\nshift\r\nset \"CWTEST10=[%~9]\"\r\nshift\r\nset \"CWTEST11=[%~9]\"\r\n> \"%CWOUT%\" set CWTEST\r\n"
	if err := os.WriteFile(target, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{output, "ARG A", "ARG|B", "ARG&B", "ARG^B", "ARG<B", "ARG>B", "ARG(B)", "ARG%PATH%", "ARG!B", "", ""}
	final, err := resolver.Resolve(root, processcontract.StartArguments{Command: filepath.ToSlash(target), Argv: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(final.Executable, filepath.Join(systemRoot, "System32", "cmd.exe")) || final.BindingPayload == "" || final.BridgePath == "" {
		t.Fatalf("batch final=%#v", final)
	}
	bridge, err := os.ReadFile(final.BridgePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range append([]string{target}, arguments...) {
		if forbidden != "" && strings.Contains(string(bridge), forbidden) {
			t.Fatalf("bridge contains caller text %q", forbidden)
		}
	}
	command := exec.Command(final.Executable, final.Argv...)
	command.Dir = final.CWD
	command.Env = final.Environment
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("batch execution failed: %v (%s)", err, combined)
	}
	payload, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for index, argument := range arguments[1:] {
		expected := "CWTEST" + leftPad(index+1) + "=[" + argument + "]"
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("missing %q in %q", expected, payload)
		}
	}
	if err := os.WriteFile(final.BridgePath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(root, processcontract.StartArguments{Command: filepath.ToSlash(target)}); err == nil || !strings.Contains(err.Error(), "BRIDGE_CHANGED") {
		t.Fatalf("tampered bridge accepted: %v", err)
	}
}

func TestBatchBridgeRejectsUnrepresentableQuoteAndNewline(t *testing.T) {
	root := t.TempDir()
	systemRoot := os.Getenv("SystemRoot")
	t.Setenv("PATH", filepath.Join(systemRoot, "System32"))
	t.Setenv("SystemRoot", systemRoot)
	resolver, _ := New(childenv.Canonical())
	target := filepath.Join(root, "target.cmd")
	if err := os.WriteFile(target, []byte("@exit /b 0\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, argument := range []string{`ARG"B`, "line1\nline2"} {
		_, err := resolver.Resolve(root, processcontract.StartArguments{Command: filepath.ToSlash(target), Argv: []string{argument}})
		if err == nil || !strings.Contains(err.Error(), "BATCH_ARGUMENT_UNREPRESENTABLE") {
			t.Fatalf("unsafe batch argument %q accepted: %v", argument, err)
		}
	}
}

func TestBatchBridgeRejectsWindowsCommandLineOverflow(t *testing.T) {
	root := t.TempDir()
	systemRoot := os.Getenv("SystemRoot")
	t.Setenv("PATH", filepath.Join(systemRoot, "System32"))
	t.Setenv("SystemRoot", systemRoot)
	resolver, _ := New(childenv.Canonical())
	target := filepath.Join(root, "target.cmd")
	if err := os.WriteFile(target, []byte("@exit /b 0\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolver.Resolve(root, processcontract.StartArguments{Command: filepath.ToSlash(target), Argv: []string{strings.Repeat("x", 30000)}})
	if err == nil || (!strings.Contains(err.Error(), "TOO_LARGE") && !strings.Contains(err.Error(), "COMMAND_LINE")) {
		t.Fatalf("oversized batch invocation accepted: %v", err)
	}
}

func leftPad(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return "" + string(rune('0'+value/10)) + string(rune('0'+value%10))
}
