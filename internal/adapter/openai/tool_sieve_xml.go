package openai

import (
	"strings"

	"ds2api/internal/util"
)

func consumeXMLToolCapture(captured string, toolNames []string) (prefix string, calls []util.ParsedToolCall, suffix string, ready bool) {
	anyOpenFound := false
	type candidate struct {
		start  int
		prefix string
		calls  []util.ParsedToolCall
		suffix string
	}
	type rejectedBlock struct {
		start  int
		prefix string
		suffix string
	}
	var best *candidate
	var rejected *rejectedBlock

	for searchFrom := 0; searchFrom < len(captured); {
		tag, ok := util.FindToolMarkupTagOutsideIgnored(captured, searchFrom)
		if !ok {
			break
		}
		if tag.Closing || tag.Name != "tool_calls" {
			searchFrom = tag.End + 1
			continue
		}
		closeTag, ok := util.FindMatchingToolMarkupClose(captured, tag)
		if !ok {
			anyOpenFound = true
			searchFrom = tag.End + 1
			continue
		}

		xmlBlock := captured[tag.Start : closeTag.End+1]
		prefixPart := captured[:tag.Start]
		suffixPart := captured[closeTag.End+1:]
		parsed := util.ParseStandaloneToolCallsDetailed(xmlBlock, toolNames)
		if len(parsed.Calls) > 0 {
			prefixPart, suffixPart = trimWrappingJSONFence(prefixPart, suffixPart)
			if best == nil || tag.Start < best.start {
				best = &candidate{start: tag.Start, prefix: prefixPart, calls: parsed.Calls, suffix: suffixPart}
			}
			break
		}
		if parsed.SawToolCallSyntax {
			if rejected == nil || tag.Start < rejected.start {
				rejected = &rejectedBlock{start: tag.Start, prefix: prefixPart, suffix: suffixPart}
			}
			searchFrom = tag.End + 1
			continue
		}
		if rejected == nil || tag.Start < rejected.start {
			rejected = &rejectedBlock{start: tag.Start, prefix: prefixPart + xmlBlock, suffix: suffixPart}
		}
		searchFrom = tag.End + 1
	}
	if best != nil {
		return best.prefix, best.calls, best.suffix, true
	}
	if anyOpenFound {
		return "", nil, "", false
	}
	if rejected != nil {
		return rejected.prefix, nil, rejected.suffix, true
	}
	if invokeTag, ok := findFirstToolMarkupTagByName(captured, 0, "invoke"); ok {
		if wrapperOpen, ok := findFirstToolMarkupTagByName(captured, 0, "tool_calls"); !ok || wrapperOpen.Start > invokeTag.Start {
			if closeTag, ok := findFirstToolMarkupTagByNameFrom(captured, invokeTag.Start+1, "tool_calls", true); ok && closeTag.Start > invokeTag.Start {
				xmlBlock := "<tool_calls>" + captured[invokeTag.Start:closeTag.End+1]
				prefixPart := captured[:invokeTag.Start]
				suffixPart := captured[closeTag.End+1:]
				parsed := util.ParseStandaloneToolCallsDetailed(xmlBlock, toolNames)
				if len(parsed.Calls) > 0 {
					prefixPart, suffixPart = trimWrappingJSONFence(prefixPart, suffixPart)
					return prefixPart, parsed.Calls, suffixPart, true
				}
				if parsed.SawToolCallSyntax {
					return prefixPart, nil, suffixPart, true
				}
				return prefixPart + captured[invokeTag.Start:closeTag.End+1], nil, suffixPart, true
			}
		}
	}
	return "", nil, "", false
}

func hasOpenXMLToolTag(captured string) bool {
	for searchFrom := 0; searchFrom < len(captured); {
		tag, ok := util.FindToolMarkupTagOutsideIgnored(captured, searchFrom)
		if !ok {
			return false
		}
		if tag.Closing || tag.Name != "tool_calls" {
			searchFrom = tag.End + 1
			continue
		}
		if _, ok := util.FindMatchingToolMarkupClose(captured, tag); !ok {
			return true
		}
		searchFrom = tag.End + 1
	}
	return false
}

func shouldKeepBareInvokeCapture(captured string) bool {
	invokeTag, ok := findFirstToolMarkupTagByName(captured, 0, "invoke")
	if !ok {
		return false
	}
	if wrapperOpen, ok := findFirstToolMarkupTagByName(captured, 0, "tool_calls"); ok && wrapperOpen.Start <= invokeTag.Start {
		return false
	}
	if closeTag, ok := findFirstToolMarkupTagByNameFrom(captured, invokeTag.Start+1, "tool_calls", true); ok && closeTag.Start > invokeTag.Start {
		return true
	}
	startEnd := invokeTag.End
	if startEnd < 0 {
		return true
	}
	body := captured[startEnd+1:]
	trimmedBody := strings.TrimLeft(body, " \t\r\n")
	if trimmedBody == "" {
		return true
	}

	if invokeCloseTag, ok := findFirstToolMarkupTagByNameFrom(captured, startEnd+1, "invoke", true); ok {
		return strings.TrimSpace(captured[invokeCloseTag.End+1:]) == ""
	}

	trimmedLower := strings.ToLower(trimmedBody)
	return strings.HasPrefix(trimmedLower, "<parameter") ||
		strings.HasPrefix(trimmedLower, "{") ||
		strings.HasPrefix(trimmedLower, "[")
}

func stripInlineToolUsePrefix(content string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "tool use:") {
		return content
	}
	rest := trimmed[len("tool use:"):]
	if strings.HasPrefix(strings.TrimLeft(rest, " \t"), "\n") {
		// Multiline form: hide marker and tool name line, keep only leading prose.
		firstNL := strings.Index(rest, "\n")
		if firstNL < 0 {
			return content
		}
		rest2 := rest[firstNL+1:]
		secondNL := strings.Index(rest2, "\n")
		if secondNL < 0 {
			return content
		}
		prefixLen := len(content) - len(trimmed) + len("tool use:") + firstNL + 1 + secondNL + 1
		if prefixLen > len(content) {
			return content
		}
		return content[:len(content)-len(trimmed)]
	}
	idx := strings.Index(rest, "(")
	if idx < 0 {
		return content
	}
	name := strings.TrimSpace(rest[:idx])
	if name == "" {
		return content
	}
	nameLen := len(trimmed) - len(strings.TrimLeft(trimmed, " \t\r\n"))
	prefixLen := nameLen + len("tool use:") + idx + 1
	if prefixLen > len(content) {
		return content
	}
	return content[:nameLen]
}

func findPartialXMLToolTagStart(s string) int {
	lastLT := strings.LastIndex(s, "<")
	if lastLT < 0 {
		return -1
	}
	start := includeDuplicateLeadingLessThan(s, lastLT)
	tail := s[start:]
	if strings.Contains(tail, ">") {
		return -1
	}
	if util.IsPartialToolMarkupTagPrefix(tail) {
		return start
	}
	return -1
}

func trimWrappingJSONFence(prefix, suffix string) (string, string) {
	trimmedPrefix := strings.TrimRight(prefix, " \t\r\n")
	fenceIdx := strings.LastIndex(trimmedPrefix, "```")
	if fenceIdx < 0 {
		return prefix, suffix
	}
	if strings.Count(trimmedPrefix[:fenceIdx+3], "```")%2 == 0 {
		return prefix, suffix
	}
	fenceHeader := strings.TrimSpace(trimmedPrefix[fenceIdx+3:])
	if fenceHeader != "" && !strings.EqualFold(fenceHeader, "json") {
		return prefix, suffix
	}

	trimmedSuffix := strings.TrimLeft(suffix, " \t\r\n")
	if !strings.HasPrefix(trimmedSuffix, "```") {
		return prefix, suffix
	}
	consumedLeading := len(suffix) - len(trimmedSuffix)
	return trimmedPrefix[:fenceIdx], suffix[consumedLeading+3:]
}
