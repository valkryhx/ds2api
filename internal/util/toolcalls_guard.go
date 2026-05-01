package util

import "strings"

// HasMalformedToolCallFragment reports whether tool-call-like markup is present
// but structurally incomplete, so callers should avoid executing parsed tool calls.
func HasMalformedToolCallFragment(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	if hasDanglingCDATA(text) || hasDanglingXMLComment(text) || hasTrailingPartialToolTag(text) {
		return true
	}
	if hasUnmatchedToolMarkupTag(text, "tool_calls") || hasUnmatchedToolMarkupTag(text, "invoke") {
		return true
	}
	return false
}

func hasDanglingCDATA(text string) bool {
	lower := strings.ToLower(text)
	return strings.Count(lower, "<![cdata[") > strings.Count(lower, "]]>")
}

func hasDanglingXMLComment(text string) bool {
	lower := strings.ToLower(text)
	return strings.Count(lower, "<!--") > strings.Count(lower, "-->")
}

func hasTrailingPartialToolTag(text string) bool {
	lastLT := strings.LastIndex(text, "<")
	if lastLT < 0 {
		return false
	}
	start := includeDuplicateLeadingLessThanLocal(text, lastLT)
	tail := text[start:]
	if tail == "" || strings.Contains(tail, ">") {
		return false
	}
	return IsPartialToolMarkupTagPrefix(tail)
}

func includeDuplicateLeadingLessThanLocal(s string, idx int) int {
	for idx > 0 && s[idx-1] == '<' {
		idx--
	}
	return idx
}

func hasUnmatchedToolMarkupTag(text, name string) bool {
	if text == "" || name == "" {
		return false
	}
	for searchFrom := 0; searchFrom < len(text); {
		tag, ok := FindToolMarkupTagOutsideIgnored(text, searchFrom)
		if !ok {
			return false
		}
		searchFrom = tag.End + 1
		if tag.Closing || tag.SelfClosing || tag.Name != name {
			continue
		}
		if _, ok := FindMatchingToolMarkupClose(text, tag); !ok {
			return true
		}
	}
	return false
}
