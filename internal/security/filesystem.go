package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ProtectedRoots(dataRoot string) []string {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil
	}
	base := filepath.Clean(dataRoot)
	values := []string{
		filepath.Join(base, "auth"),
		filepath.Join(base, "config"),
		filepath.Join(base, "temp"),
		filepath.Join(base, "tunnel"),
		filepath.Join(base, "runtime"),
	}
	sort.Slice(values, func(i, j int) bool { return strings.ToLower(values[i]) < strings.ToLower(values[j]) })
	return values
}

func PathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// CanonicalPath resolves every existing path prefix so junctions and symlinks
// cannot turn a lexically safe path into a protected-path escape. A missing
// leaf is retained below the nearest existing canonical parent.
func CanonicalPath(value, cwd string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("SECURITY_PATH_INVALID")
	}
	candidate := filepath.FromSlash(value)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(cwd, candidate)
	}
	absolute, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return "", fmt.Errorf("SECURITY_PATH_INVALID: %w", err)
	}
	missing := make([]string, 0, 4)
	probe := absolute
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("SECURITY_PATH_RESOLVE_FAILED: %w", statErr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("SECURITY_PATH_RESOLVE_FAILED")
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", fmt.Errorf("SECURITY_PATH_RESOLVE_FAILED: %w", err)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	return filepath.Clean(resolved), nil
}

func topLevelPath(argument, cwd string) (string, bool) {
	if argument == "" || strings.ContainsAny(argument, "\r\n\x00") {
		return "", false
	}
	candidateValue := argument
	if strings.HasPrefix(candidateValue, "-") {
		if _, value, ok := strings.Cut(candidateValue, "="); ok {
			candidateValue = value
		}
	}
	candidate := filepath.FromSlash(candidateValue)
	if !filepath.IsAbs(candidate) && !strings.HasPrefix(candidate, "."+string(filepath.Separator)) && !strings.HasPrefix(candidate, ".."+string(filepath.Separator)) {
		return "", false
	}
	resolved, err := CanonicalPath(candidate, cwd)
	return resolved, err == nil
}

func protectedPath(path, dataRoot string) bool {
	for _, root := range ProtectedRoots(dataRoot) {
		if PathWithin(path, root) {
			return true
		}
	}
	return false
}
