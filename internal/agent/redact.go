package agent

import "regexp"

var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\bsk_[A-Za-z0-9][A-Za-z0-9_-]{8,}\b`),
}

func redactCredentials(s string) string {
	for _, pattern := range credentialPatterns {
		s = pattern.ReplaceAllString(s, "sk_<redacted>")
	}
	return s
}
