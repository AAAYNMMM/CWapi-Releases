//go:build windows

package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessScopeKillsSpawnedDescendant(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Skip("pwsh.exe unavailable")
	}
	root := t.TempDir()
	childScript := filepath.Join(root, "child.ps1")
	parentScript := filepath.Join(root, "parent.ps1")
	heartbeat := filepath.Join(root, "heartbeat.txt")
	pidFile := filepath.Join(root, "child.pid")
	if err := os.WriteFile(childScript, []byte(`param([string]$Heartbeat)
while ($true) {
    [IO.File]::WriteAllText($Heartbeat, [DateTime]::UtcNow.Ticks.ToString())
    Start-Sleep -Milliseconds 100
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentScript, []byte(`param([string]$ChildScript,[string]$Heartbeat,[string]$PidFile)
Start-Sleep -Seconds 1
$child = Start-Process -FilePath 'pwsh.exe' -ArgumentList @('-NoLogo','-NoProfile','-File',$ChildScript,'-Heartbeat',$Heartbeat) -PassThru
[IO.File]::WriteAllText($PidFile, $child.Id.ToString())
while ($true) { Start-Sleep -Milliseconds 200 }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(pwsh, "-NoLogo", "-NoProfile", "-File", parentScript, "-ChildScript", childScript, "-Heartbeat", heartbeat, "-PidFile", pidFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = exec.Command("taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		_, _ = cmd.Process.Wait()
	}()

	scope, err := attachProcessScope(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if payload, readErr := os.ReadFile(pidFile); readErr == nil && strings.TrimSpace(string(payload)) != "" {
			if _, heartbeatErr := os.Stat(heartbeat); heartbeatErr == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	pidPayload, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child pid was not published: %v", err)
	}
	childPID := strings.TrimSpace(string(pidPayload))
	if childPID == "" {
		t.Fatal("child pid was empty")
	}
	before, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatalf("heartbeat was not created: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(700 * time.Millisecond)
	after, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if after.ModTime().After(before.ModTime()) {
		t.Fatalf("descendant %s continued heartbeat after job close: before=%s after=%s", childPID, before.ModTime(), after.ModTime())
	}
}
