package buildinfo

import (
	"runtime/debug"
	"strings"
)

const Version = "2.0.5"

// SourceCommit is injected by the production build with -ldflags -X.
// Development builds fall back to Go's embedded VCS metadata when available.
var SourceCommit string

func Commit() string {
	if commit := normalizeCommit(SourceCommit); commit != "" {
		return commit
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			if commit := normalizeCommit(setting.Value); commit != "" {
				return commit
			}
		}
	}
	return "unknown"
}

func normalizeCommit(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 40 {
		return ""
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return value
}
