package claude

import (
	"fmt"
	"strings"
	"time"

	"ds2api/internal/util"
)

func DetectClaudeToolCalls(finalText, finalThinking string, toolNames []string) util.ToolCallParseResult {
	textParsed := util.ParseStandaloneToolCallsDetailed(finalText, toolNames)
	if len(textParsed.Calls) > 0 {
		return textParsed
	}
	// Preserve existing behavior: when visible text exists, do not fallback to
	// thinking for tool-use extraction.
	if strings.TrimSpace(finalText) != "" {
		return textParsed
	}
	if strings.TrimSpace(finalThinking) == "" {
		return textParsed
	}
	thinkingParsed := util.ParseStandaloneToolCallsDetailed(finalThinking, toolNames)
	if len(thinkingParsed.Calls) > 0 {
		return thinkingParsed
	}
	return textParsed
}

func BuildMessageResponse(messageID, model string, normalizedMessages []any, finalThinking, finalText string, toolNames []string, toolsRaw ...any) map[string]any {
	detect := DetectClaudeToolCalls(finalText, finalThinking, toolNames)
	detected := util.NormalizeToolCallInputsForExecution(detect.Calls)
	detected = util.NormalizeParsedToolCallsForSchemas(detected, firstOptionalToolArg(toolsRaw))
	var droppedByRequired bool
	detected, droppedByRequired = util.FilterParsedToolCallsByRequiredSchemasDetailed(detected, firstOptionalToolArg(toolsRaw))
	content := make([]map[string]any, 0, 4)
	if finalThinking != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": finalThinking})
	}
	stopReason := "end_turn"
	if len(detected) > 0 {
		stopReason = "tool_use"
		for i, tc := range detected {
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    fmt.Sprintf("toolu_%d_%d", time.Now().Unix(), i),
				"name":  tc.Name,
				"input": tc.Input,
			})
		}
	} else {
		if droppedByRequired && detect.SawToolCallSyntax {
			finalText = "工具调用缺少必填参数，已拒绝执行。请重新发起请求并明确提供完整参数。"
		} else if finalText == "" {
			finalText = "抱歉，没有生成有效的响应内容。"
		}
		content = append(content, map[string]any{"type": "text", "text": finalText})
	}
	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  util.EstimateTokens(fmt.Sprintf("%v", normalizedMessages)),
			"output_tokens": util.EstimateTokens(finalThinking) + util.EstimateTokens(finalText),
		},
	}
}

func firstOptionalToolArg(values []any) any {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}
