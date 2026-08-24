package processcontract

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	MaxCommandBytes = 32768
	MaxArgvItems    = 256
	MaxArgBytes     = 32768
	MaxArgvBytes    = 131072
	MaxCWDBytes     = 512
)

var ProcessIDPattern = regexp.MustCompile(`^proc-[0-9a-f]{24}$`)

type StartArguments struct {
	Command string
	Argv    []string
	CWD     string
}

func DecodeStart(value map[string]any) (StartArguments, error) {
	if !onlyKeys(value, "command", "argv", "cwd") {
		return StartArguments{}, errors.New("MCP_PROCESS_ARGUMENTS_INVALID")
	}
	command, ok := value["command"].(string)
	if !ok || command == "" || command != strings.TrimSpace(command) || len([]byte(command)) > MaxCommandBytes ||
		strings.ContainsRune(command, 0) || strings.HasPrefix(command, `"`) || strings.HasSuffix(command, `"`) ||
		strings.HasPrefix(command, `'`) || strings.HasSuffix(command, `'`) {
		return StartArguments{}, errors.New("MCP_PROCESS_COMMAND_INVALID")
	}
	if strings.Contains(command, `\`) {
		return StartArguments{}, errors.New("MCP_PROCESS_COMMAND_PATH_INVALID")
	}
	result := StartArguments{Command: command}
	if raw, exists := value["argv"]; exists {
		items, ok := raw.([]any)
		if !ok || len(items) > MaxArgvItems {
			return StartArguments{}, errors.New("MCP_PROCESS_ARGV_INVALID")
		}
		result.Argv = make([]string, len(items))
		total := 0
		for index, rawItem := range items {
			item, ok := rawItem.(string)
			if !ok || len([]byte(item)) > MaxArgBytes || strings.ContainsRune(item, 0) {
				return StartArguments{}, fmt.Errorf("MCP_PROCESS_ARGV_INVALID: item %d", index)
			}
			total += len([]byte(item))
			if total > MaxArgvBytes {
				return StartArguments{}, errors.New("MCP_PROCESS_ARGV_INVALID: total")
			}
			result.Argv[index] = item
		}
	}
	if raw, exists := value["cwd"]; exists {
		cwd, ok := raw.(string)
		if !ok || len([]rune(cwd)) > MaxCWDBytes || strings.ContainsRune(cwd, 0) || !validRemoteCWD(cwd) {
			return StartArguments{}, errors.New("MCP_PROCESS_CWD_INVALID")
		}
		result.CWD = cwd
	}
	return result, nil
}

func validRemoteCWD(value string) bool {
	if value == "" || value == "." {
		return true
	}
	if value != strings.TrimSpace(value) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	if len(value) >= 2 && value[1] == ':' {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func DecodeProcessID(value map[string]any) (string, error) {
	if !onlyKeys(value, "process_id") || len(value) != 1 {
		return "", errors.New("MCP_PROCESS_ARGUMENTS_INVALID")
	}
	processID, ok := value["process_id"].(string)
	if !ok || !ProcessIDPattern.MatchString(processID) {
		return "", errors.New("MCP_PROCESS_ID_INVALID")
	}
	return processID, nil
}

func onlyKeys(value map[string]any, allowed ...string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range value {
		if _, ok := set[key]; !ok {
			return false
		}
	}
	return true
}
