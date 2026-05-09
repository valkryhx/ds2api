package sse

import "fmt"

// LineResult is the normalized parse result for one DeepSeek SSE line.
type LineResult struct {
	Parsed        bool
	Stop          bool
	ContentFilter bool
	ErrorMessage  string
	ErrorCode     string
	Parts         []ContentPart
	NextType      string
}

// ParseDeepSeekContentLine centralizes one-line DeepSeek SSE parsing for both
// streaming and non-streaming handlers.
func ParseDeepSeekContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed {
		return LineResult{NextType: currentType}
	}
	if done {
		return LineResult{Parsed: true, Stop: true, NextType: currentType}
	}
	if errObj, hasErr := chunk["error"]; hasErr {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: fmt.Sprintf("%v", errObj),
			NextType:     currentType,
		}
	}
	if code, _ := chunk["code"].(string); code == "content_filter" {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			ErrorMessage:  "content filtered by upstream",
			ErrorCode:     code,
			NextType:      currentType,
		}
	}
	if isDeepSeekHintError(chunk) {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: deepSeekHintErrorMessage(chunk),
			ErrorCode:    deepSeekHintErrorCode(chunk),
			NextType:     currentType,
		}
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, thinkingEnabled, currentType)
	return LineResult{
		Parsed:   true,
		Stop:     finished,
		Parts:    parts,
		NextType: nextType,
	}
}

func isDeepSeekHintError(chunk map[string]any) bool {
	typ, _ := chunk["type"].(string)
	if typ == "error" {
		return true
	}
	return deepSeekHintErrorCode(chunk) != ""
}

func deepSeekHintErrorCode(chunk map[string]any) string {
	for _, key := range []string{"finish_reason", "code"} {
		if code, _ := chunk[key].(string); code != "" {
			return code
		}
	}
	return ""
}

func deepSeekHintErrorMessage(chunk map[string]any) string {
	for _, key := range []string{"content", "message", "msg"} {
		if msg, _ := chunk[key].(string); msg != "" {
			return msg
		}
	}
	if code := deepSeekHintErrorCode(chunk); code != "" {
		return code
	}
	return "upstream error"
}
