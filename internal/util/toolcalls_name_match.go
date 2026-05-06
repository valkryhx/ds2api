package util

import (
	"regexp"
	"strings"
)

var toolNameLoosePattern = regexp.MustCompile(`[^a-z0-9]+`)

var shellLikeToolAliases = []string{
	"bash",
	"shell",
	"shell_command",
	"execute_command",
	"powershell",
	"cmd",
	"terminal",
}

var readLikeToolAliases = []string{
	"read",
	"read_file",
	"cat",
}

func resolveAllowedToolNameWithLooseMatch(name string, allowed map[string]struct{}, allowedCanonical map[string]string) string {
	if _, ok := allowed[name]; ok {
		return name
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := allowedCanonical[lower]; ok {
		return canonical
	}
	if idx := strings.LastIndex(lower, "."); idx >= 0 && idx < len(lower)-1 {
		if canonical, ok := allowedCanonical[lower[idx+1:]]; ok {
			return canonical
		}
	}
	loose := toolNameLoosePattern.ReplaceAllString(lower, "")
	if loose == "" {
		return ""
	}
	if canonical := resolveAllowedToolAliasFamily(lower, allowedCanonical); canonical != "" {
		return canonical
	}
	for candidateLower, canonical := range allowedCanonical {
		if toolNameLoosePattern.ReplaceAllString(candidateLower, "") == loose {
			return canonical
		}
	}
	return ""
}

func resolveAllowedToolAliasFamily(name string, allowedCanonical map[string]string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	name = collapseToolNameNamespace(name)
	if canonical := resolveAliasFamily(name, shellLikeToolAliases, allowedCanonical); canonical != "" {
		return canonical
	}
	if canonical := resolveAliasFamily(name, readLikeToolAliases, allowedCanonical); canonical != "" {
		return canonical
	}
	return ""
}

func resolveAliasFamily(name string, family []string, allowedCanonical map[string]string) string {
	if !aliasFamilyContains(name, family) {
		return ""
	}
	for candidateLower, canonical := range allowedCanonical {
		collapsed := collapseToolNameNamespace(candidateLower)
		if aliasFamilyContains(collapsed, family) {
			return canonical
		}
	}
	return ""
}

func aliasFamilyContains(name string, family []string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, alias := range family {
		if name == alias {
			return true
		}
	}
	return false
}
