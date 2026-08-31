package service

import (
	"os"
	"path/filepath"
	"runtime"
)

func discoverGitExecutable(dataRoot string) string {
	roots := []string{filepath.Dir(filepath.Clean(dataRoot))}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	seen := map[string]struct{}{}
	for _, root := range roots {
		for _, relative := range []string{filepath.Join("runtime", "git", "cmd", name), filepath.Join("runtime", "git", "bin", name)} {
			candidate := filepath.Clean(filepath.Join(root, relative))
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	return ""
}

func discoverTunnelExecutable(dataRoot string) string {
	roots := []string{filepath.Dir(filepath.Clean(dataRoot))}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	name := "tunnel-client"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	seen := map[string]struct{}{}
	for _, root := range roots {
		for _, relative := range []string{filepath.Join("runtime", "tunnel", "current", name), filepath.Join("runtime", "tunnel", name)} {
			candidate := filepath.Clean(filepath.Join(root, relative))
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	return ""
}
