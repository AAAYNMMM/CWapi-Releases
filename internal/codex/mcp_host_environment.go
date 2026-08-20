package codex

import "os"

var appServerEnvironmentKeys = []string{
	"PATH",
	"SystemRoot",
	"WINDIR",
	"COMSPEC",
	"PATHEXT",
	"TEMP",
	"TMP",
	"USERPROFILE",
	"HOME",
	"APPDATA",
	"LOCALAPPDATA",
}

func appServerEnvironment() []string {
	result := make([]string, 0, len(appServerEnvironmentKeys))
	for _, key := range appServerEnvironmentKeys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}
