//go:build windows

package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
)

func createDirectoryLink(target, link string) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	}
	command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", link, target)
	return command.Run()
}
