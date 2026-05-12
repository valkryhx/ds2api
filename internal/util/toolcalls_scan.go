package util

import "strings"

type toolMarkupNameAlias struct {
	raw       string
	canonical string
	dsmlOnly  bool
}

var toolMarkupNames = []toolMarkupNameAlias{
	{raw: "tool_calls", canonical: "tool_calls"},
	{raw: "tool-calls", canonical: "tool_calls", dsmlOnly: true},
	{raw: "toolcalls", canonical: "tool_calls", dsmlOnly: true},
	{raw: "invoke", canonical: "invoke"},
	{raw: "parameter", canonical: "parameter"},
}

type ToolMarkupTag struct {
	Start       int
	End         int
	NameStart   int
	NameEnd     int
	Name        string
	Closing     bool
	SelfClosing bool
	DSMLLike    bool
	Canonical   bool
}

func ContainsToolMarkupSyntaxOutsideIgnored(text string) (hasDSML, hasCanonical bool) {
	lower := strings.ToLower(text)
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return hasDSML, hasCanonical
		}
		if advanced {
			i = next
			continue
		}
		if tag, ok := scanToolMarkupTagAt(text, i); ok {
			if tag.DSMLLike {
				hasDSML = true
			} else {
				hasCanonical = true
			}
			if hasDSML && hasCanonical {
				return true, true
			}
			i = tag.End + 1
			continue
		}
		i++
	}
	return hasDSML, hasCanonical
}

func ContainsToolCallWrapperSyntaxOutsideIgnored(text string) (hasDSML, hasCanonical bool) {
	lower := strings.ToLower(text)
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return hasDSML, hasCanonical
		}
		if advanced {
			i = next
			continue
		}
		if tag, ok := scanToolMarkupTagAt(text, i); ok {
			if tag.Name != "tool_calls" {
				i = tag.End + 1
				continue
			}
			if tag.DSMLLike {
				hasDSML = true
			} else {
				hasCanonical = true
			}
			if hasDSML && hasCanonical {
				return true, true
			}
			i = tag.End + 1
			continue
		}
		i++
	}
	return hasDSML, hasCanonical
}

func FindToolMarkupTagOutsideIgnored(text string, start int) (ToolMarkupTag, bool) {
	lower := strings.ToLower(text)
	for i := maxInt(start, 0); i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return ToolMarkupTag{}, false
		}
		if advanced {
			i = next
			continue
		}
		if tag, ok := scanToolMarkupTagAt(text, i); ok {
			return tag, true
		}
		i++
	}
	return ToolMarkupTag{}, false
}

func FindMatchingToolMarkupClose(text string, open ToolMarkupTag) (ToolMarkupTag, bool) {
	if text == "" || open.Name == "" || open.Closing {
		return ToolMarkupTag{}, false
	}
	depth := 1
	for pos := open.End + 1; pos < len(text); {
		tag, ok := FindToolMarkupTagOutsideIgnored(text, pos)
		if !ok {
			return ToolMarkupTag{}, false
		}
		if tag.Name != open.Name {
			pos = tag.End + 1
			continue
		}
		if tag.Closing {
			depth--
			if depth == 0 {
				return tag, true
			}
		} else if !tag.SelfClosing {
			depth++
		}
		pos = tag.End + 1
	}
	return ToolMarkupTag{}, false
}

func scanToolMarkupTagAt(text string, start int) (ToolMarkupTag, bool) {
	if start < 0 || start >= len(text) {
		return ToolMarkupTag{}, false
	}
	lower := strings.ToLower(text)
	i := start
	syntheticLessThan := false
	if text[start] == '<' {
		i++
		for i < len(text) && text[i] == '<' {
			i++
		}
	} else {
		if !isMissingLeadingLessThanDSMLTagStart(text, start) {
			return ToolMarkupTag{}, false
		}
		syntheticLessThan = true
	}
	closing := false
	if i < len(text) && text[i] == '/' {
		closing = true
		i++
	}
	prefixStart := i
	i, dsmlLike := consumeToolMarkupNamePrefix(lower, text, i)
	if syntheticLessThan && !dsmlLike {
		return ToolMarkupTag{}, false
	}
	name, nameLen := matchToolMarkupName(lower, i, dsmlLike)
	if nameLen == 0 {
		fallbackName, fallbackStart, fallbackLen, ok := matchToolMarkupNameAfterArbitraryPrefix(lower, text, prefixStart)
		if !ok {
			return ToolMarkupTag{}, false
		}
		if !closing && strings.Contains(text[prefixStart:fallbackStart], "/") {
			closing = true
		}
		name = fallbackName
		i = fallbackStart
		nameLen = fallbackLen
		dsmlLike = true
	}
	nameEnd := i + nameLen
	nameEndBeforePipes := nameEnd
	for next, ok := consumeToolMarkupPipe(text, nameEnd); ok; next, ok = consumeToolMarkupPipe(text, nameEnd) {
		nameEnd = next
	}
	hasTrailingPipe := nameEnd > nameEndBeforePipes
	if !hasToolMarkupBoundary(text, nameEnd) {
		return ToolMarkupTag{}, false
	}
	end := findXMLTagEnd(text, nameEnd)
	if end < 0 {
		if !hasTrailingPipe {
			return ToolMarkupTag{}, false
		}
		end = nameEnd - 1
	}
	if hasTrailingPipe {
		if nextLT := strings.IndexByte(text[nameEnd:], '<'); nextLT >= 0 && end >= nameEnd+nextLT {
			end = nameEnd - 1
		}
	}
	trimmed := strings.TrimSpace(text[start : end+1])
	return ToolMarkupTag{
		Start:       start,
		End:         end,
		NameStart:   i,
		NameEnd:     nameEnd,
		Name:        name,
		Closing:     closing,
		SelfClosing: strings.HasSuffix(trimmed, "/>"),
		DSMLLike:    dsmlLike,
		Canonical:   !dsmlLike,
	}, true
}

func IsPartialToolMarkupTagPrefix(text string) bool {
	if text == "" || strings.Contains(text, ">") {
		return false
	}
	lower := strings.ToLower(text)
	i := 0
	if text[0] == '<' {
		i = 1
		for i < len(text) && text[i] == '<' {
			i++
		}
	} else if !isMissingLeadingLessThanDSMLTagStart(text, 0) {
		return false
	}
	if i >= len(text) {
		return true
	}
	if text[i] == '/' {
		i++
	}
	for i <= len(text) {
		if i == len(text) {
			return true
		}
		if hasToolMarkupNamePrefix(lower[i:]) {
			return true
		}
		if hasDSMLNamePrefixOrPartial(lower[i:]) {
			return true
		}
		if hasPartialToolMarkupNameAfterArbitraryPrefix(lower, text, i) {
			return true
		}
		next, ok := consumeToolMarkupNamePrefixOnce(lower, text, i)
		if !ok {
			return false
		}
		i = next
	}
	return false
}

func consumeToolMarkupNamePrefix(lower, text string, idx int) (int, bool) {
	dsmlLike := false
	for {
		next, ok := consumeToolMarkupNamePrefixOnce(lower, text, idx)
		if !ok {
			return idx, dsmlLike
		}
		idx = next
		dsmlLike = true
	}
}

func consumeToolMarkupNamePrefixOnce(lower, text string, idx int) (int, bool) {
	if next, ok := consumeToolMarkupPipe(text, idx); ok {
		return next, true
	}
	if idx < len(text) && (text[idx] == ' ' || text[idx] == '\t' || text[idx] == '\r' || text[idx] == '\n') {
		return idx + 1, true
	}
	if strings.HasPrefix(lower[idx:], "dsml") {
		next := idx + len("dsml")
		if next < len(text) && (text[next] == '_' || text[next] == '-') {
			next++
		}
		return next, true
	}
	if next, ok := consumeArbitraryToolMarkupNamePrefix(lower, text, idx); ok {
		return next, true
	}
	return idx, false
}

func consumeArbitraryToolMarkupNamePrefix(lower, text string, idx int) (int, bool) {
	nextSegment, ok := consumeToolMarkupPrefixSegment(lower, idx)
	if !ok {
		return idx, false
	}
	j := nextSegment
	for {
		nextSegment, ok = consumeToolMarkupPrefixSegment(lower, j)
		if !ok {
			break
		}
		j = nextSegment
	}
	k := skipToolMarkupSpaces(text, j)
	next, ok := consumeToolMarkupPipe(text, k)
	if !ok && k < len(text) && (text[k] == '_' || text[k] == '-') {
		next = k + 1
		ok = true
	}
	if !ok {
		return idx, false
	}
	next = skipToolMarkupSpaces(text, next)
	if !hasToolMarkupNamePrefix(lower[next:]) {
		return idx, false
	}
	return next, true
}

func consumeToolMarkupPrefixSegment(lower string, idx int) (int, bool) {
	if idx < 0 || idx >= len(lower) {
		return idx, false
	}
	ch := lower[idx]
	if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
		return idx + 1, true
	}
	return idx, false
}

func skipToolMarkupSpaces(text string, idx int) int {
	for idx < len(text) {
		switch text[idx] {
		case ' ', '\t', '\r', '\n':
			idx++
		default:
			return idx
		}
	}
	return idx
}

func hasToolMarkupNamePrefix(lowerTail string) bool {
	for _, alias := range toolMarkupNames {
		if strings.HasPrefix(lowerTail, alias.raw) || strings.HasPrefix(alias.raw, lowerTail) {
			return true
		}
	}
	return false
}

func matchToolMarkupName(lower string, start int, dsmlLike bool) (string, int) {
	for _, alias := range toolMarkupNames {
		if alias.dsmlOnly && !dsmlLike {
			continue
		}
		if strings.HasPrefix(lower[start:], alias.raw) {
			return alias.canonical, len(alias.raw)
		}
	}
	return "", 0
}

func matchToolMarkupNameAfterArbitraryPrefix(lower, text string, start int) (string, int, int, bool) {
	for idx := start; idx < len(text); idx++ {
		if isToolMarkupTagTerminator(text, idx) {
			return "", 0, 0, false
		}
		for _, alias := range toolMarkupNames {
			if !strings.HasPrefix(lower[idx:], alias.raw) {
				continue
			}
			if !toolMarkupPrefixAllowsLocalName(text[start:idx]) {
				continue
			}
			return alias.canonical, idx, len(alias.raw), true
		}
	}
	return "", 0, 0, false
}

func hasPartialToolMarkupNameAfterArbitraryPrefix(lower, text string, start int) bool {
	for idx := start; idx < len(text); idx++ {
		if isToolMarkupTagTerminator(text, idx) {
			return false
		}
		if toolMarkupPrefixAllowsLocalName(text[start:idx]) && hasToolMarkupNamePrefix(lower[idx:]) {
			return true
		}
		if toolMarkupPrefixAllowsLocalName(text[start:idx]) && hasDSMLNamePrefixOrPartial(lower[idx:]) {
			return true
		}
	}
	return toolMarkupPrefixAllowsLocalName(text[start:])
}

func hasDSMLNamePrefixOrPartial(lowerTail string) bool {
	return strings.HasPrefix(lowerTail, "dsml") || strings.HasPrefix("dsml", lowerTail)
}

func toolMarkupPrefixAllowsLocalName(prefix string) bool {
	if prefix == "" {
		return false
	}
	lower := strings.ToLower(prefix)
	if strings.Contains(lower, "dsml") {
		return true
	}
	if strings.ContainsAny(prefix, "=\"'") {
		return false
	}
	last := prefix[len(prefix)-1]
	return !((last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') || (last >= '0' && last <= '9'))
}

func isToolMarkupTagTerminator(text string, idx int) bool {
	if idx >= len(text) {
		return false
	}
	return text[idx] == '>'
}

func consumeToolMarkupPipe(text string, idx int) (int, bool) {
	if idx >= len(text) {
		return idx, false
	}
	if text[idx] == '|' {
		return idx + 1, true
	}
	if strings.HasPrefix(text[idx:], "｜") {
		return idx + len("｜"), true
	}
	return idx, false
}

func isMissingLeadingLessThanDSMLTagStart(text string, start int) bool {
	if start < 0 || start >= len(text) || !hasLooseToolMarkupBoundaryBefore(text, start) {
		return false
	}
	switch text[start] {
	case '|':
		return true
	case '/':
		if start+1 < len(text) && text[start+1] == '|' {
			return true
		}
		if strings.HasPrefix(text[start+1:], "｜") {
			return true
		}
		return false
	default:
		if strings.HasPrefix(text[start:], "｜") {
			return true
		}
		return false
	}
}

func hasLooseToolMarkupBoundaryBefore(text string, start int) bool {
	if start <= 0 {
		return true
	}
	prev := text[start-1]
	if prev >= 0x80 {
		return true
	}
	switch prev {
	case ' ', '\t', '\n', '\r', '.', ',', ';', ':', '!', '?', '(', ')', '[', ']', '{', '}', '\'', '"', '`', '>':
		return true
	default:
		return false
	}
}

func hasToolMarkupBoundary(text string, idx int) bool {
	if idx >= len(text) {
		return true
	}
	switch text[idx] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	default:
		return false
	}
}

func skipXMLIgnoredSection(lower string, i int) (next int, advanced bool, blocked bool) {
	switch {
	case strings.HasPrefix(lower[i:], "<![cdata["):
		end := strings.Index(lower[i+len("<![cdata["):], "]]>")
		if end < 0 {
			if recoveredEnd, ok := findImplicitCDATARecoveryEnd(lower, i+len("<![cdata[")); ok {
				return recoveredEnd, true, false
			}
			return 0, false, true
		}
		return i + len("<![cdata[") + end + len("]]>"), true, false
	case strings.HasPrefix(lower[i:], "<!--"):
		end := strings.Index(lower[i+len("<!--"):], "-->")
		if end < 0 {
			return 0, false, true
		}
		return i + len("<!--") + end + len("-->"), true, false
	default:
		return i, false, false
	}
}

func findImplicitCDATARecoveryEnd(lower string, from int) (int, bool) {
	if from < 0 || from >= len(lower) {
		return 0, false
	}
	candidates := []string{
		"</|dsml|parameter>",
		"</parameter>",
		"</|dsml|argument>",
		"</argument>",
	}
	best := -1
	for _, marker := range candidates {
		if idx := strings.Index(lower[from:], marker); idx >= 0 {
			abs := from + idx
			if best < 0 || abs < best {
				best = abs
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

func findXMLTagEnd(text string, from int) int {
	quote := byte(0)
	for i := maxInt(from, 0); i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return i
		}
	}
	return -1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
