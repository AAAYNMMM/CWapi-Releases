package childenv

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const FixedPathExt = ".COM;.EXE;.BAT;.CMD"

var inheritedKeys = []string{
	"PATH",
	"SystemRoot",
	"WINDIR",
	"TEMP",
	"TMP",
	"USERPROFILE",
	"USERNAME",
	"APPDATA",
	"LOCALAPPDATA",
}

// Canonical returns the bounded environment shared by CWapi-owned child
// processes. Credentials and debug toggles are excluded by construction.
func Canonical() []string {
	values := make(map[string]string, len(inheritedKeys)+2)
	for _, key := range inheritedKeys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			values[key] = value
		}
	}
	root := values["SystemRoot"]
	if root == "" {
		root = values["WINDIR"]
	}
	if root != "" {
		values["COMSPEC"] = filepath.Join(root, "System32", "cmd.exe")
	}
	values["PATHEXT"] = FixedPathExt
	return ordered(values)
}

// Git returns a non-interactive Git/credential-helper environment that cannot inherit
// host credentials, trace flags, or global Git configuration.
func Git(ghConfigDir string) []string {
	values := parse(Canonical())
	values["GIT_TERMINAL_PROMPT"] = "0"
	values["GIT_CONFIG_NOSYSTEM"] = "1"
	values["GIT_CONFIG_GLOBAL"] = os.DevNull
	values["GCM_INTERACTIVE"] = "Never"
	if value := strings.TrimSpace(ghConfigDir); value != "" {
		values["GH_CONFIG_DIR"] = filepath.Clean(value)
	}
	return ordered(values)
}

func Value(entries []string, target string) string {
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, target) {
			return value
		}
	}
	return ""
}

func Merge(entries []string, overrides map[string]*string) []string {
	values := parse(entries)
	for target, replacement := range overrides {
		for existing := range values {
			if strings.EqualFold(existing, target) {
				delete(values, existing)
			}
		}
		if replacement != nil {
			values[target] = *replacement
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func parse(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			values[key] = value
		}
	}
	return values
}

func ordered(values map[string]string) []string {
	order := append(append([]string(nil), inheritedKeys...),
		"COMSPEC", "PATHEXT", "GIT_TERMINAL_PROMPT", "GIT_CONFIG_NOSYSTEM",
		"GIT_CONFIG_GLOBAL", "GCM_INTERACTIVE", "GH_CONFIG_DIR")
	result := make([]string, 0, len(values))
	for _, key := range order {
		if value, ok := values[key]; ok {
			result = append(result, key+"="+value)
		}
	}
	return result
}
