package repository

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Identity struct {
	Repository    string
	NormalizedURL string
}

// Parse accepts only an ASCII GitHub HTTPS owner/repository URL. It is the
// single normalization boundary used by mirrors, fingerprints, Tokens and logs.
func Parse(raw string) (Identity, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || len(raw) > 2048 {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	for index := 0; index < len(raw); index++ {
		if raw[index] < 0x21 || raw[index] > 0x7e || raw[index] == '\\' || raw[index] == '%' {
			return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Host, "github.com") {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	if parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	if !strings.HasPrefix(parsed.Path, "/") || strings.HasSuffix(parsed.Path, "/") {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	owner, name := parts[0], parts[1]
	if len(name) > 4 && strings.EqualFold(name[len(name)-4:], ".git") {
		name = name[:len(name)-4]
	}
	if owner == "" || name == "" || !segmentPattern.MatchString(owner) || !segmentPattern.MatchString(name) {
		return Identity{}, errors.New("MCP_REPOSITORY_URL_INVALID")
	}
	identity := strings.ToLower(owner + "/" + name)
	return Identity{
		Repository:    identity,
		NormalizedURL: fmt.Sprintf("https://github.com/%s/%s", owner, name),
	}, nil
}
