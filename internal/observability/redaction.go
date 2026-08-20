package observability

import "regexp"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(xapp|xox[baprs])-[-A-Za-z0-9]+\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]+\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*`),
}

var keyedSecretPattern = regexp.MustCompile(`(?i)\b(token|secret|password|authorization|api[_-]?key)\s*[:=]\s*([^\s,;]+)`)

// Redact removes common secret forms before data reaches SQLite, live memory,
// the GUI, logs or result evidence.
func Redact(value string) string {
	redacted := value
	for _, pattern := range secretPatterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	redacted = keyedSecretPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
	return redacted
}
