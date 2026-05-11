package claude

import (
	"encoding/json"
	"fmt"
	"time"

	"ds2api/internal/devcapture"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"
)

func (s *claudeStreamRuntime) closeThinkingBlock() {
	if !s.thinkingBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.thinkingBlockIndex,
	})
	s.thinkingBlockOpen = false
	s.thinkingBlockIndex = -1
}

func (s *claudeStreamRuntime) closeTextBlock() {
	if !s.textBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.textBlockIndex,
	})
	s.textBlockOpen = false
	s.textBlockIndex = -1
}

func (s *claudeStreamRuntime) finalize(stopReason string) {
	if s.ended {
		return
	}
	s.ended = true

	s.closeThinkingBlock()
	s.closeTextBlock()

	finalThinking := s.thinking.String()
	finalText := s.text.String()
	rawThinking := s.thinkingRaw.String()

	if s.bufferToolContent {
		detected := []util.ParsedToolCall(nil)
		textParsed := util.ToolCallParseResult{}
		thinkingParsed := util.ToolCallParseResult{}
		parseToolNames := util.PermissiveToolParseNames(s.toolNames)
		textParsed = util.ParseStandaloneToolCallsDetailed(finalText, parseToolNames)
		textParsed.Calls = util.CanonicalizeParsedToolCallNames(textParsed.Calls, s.toolNames)
		detected = s.prepareToolCallsForExecution(textParsed.Calls)
		if len(detected) == 0 && finalText == "" && rawThinking != "" {
			thinkingParsed = util.ParseStandaloneToolCallsDetailed(rawThinking, parseToolNames)
			thinkingParsed.Calls = util.CanonicalizeParsedToolCallNames(thinkingParsed.Calls, s.toolNames)
			detected = s.prepareToolCallsForExecution(thinkingParsed.Calls)
		}
		s.logToolCallDebug(stopReason, textParsed, thinkingParsed)
		if len(detected) > 0 {
			stopReason = "tool_use"
			for i, tc := range detected {
				recordClaudeStreamToolUse(tc, stopReason, s.toolNames)
				idx := s.nextBlockIndex + i
				inputJSON, _ := json.Marshal(tc.Input)
				s.send("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    fmt.Sprintf("toolu_%d_%d", time.Now().Unix(), idx),
						"name":  tc.Name,
						"input": map[string]any{},
					},
				})
				if len(inputJSON) > 0 && string(inputJSON) != "{}" {
					s.send("content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": idx,
						"delta": map[string]any{
							"type":         "input_json_delta",
							"partial_json": string(inputJSON),
						},
					})
				}
				s.send("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": idx,
				})
			}
			s.nextBlockIndex += len(detected)
		} else if finalText != "" {
			if s.droppedInvalidTool {
				finalText = "工具调用缺少必填参数，已拒绝执行。请重新发起请求并明确提供完整参数。"
			}
			idx := s.nextBlockIndex
			s.nextBlockIndex++
			s.send("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			s.send("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{
					"type": "text_delta",
					"text": finalText,
				},
			})
			s.send("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": idx,
			})
		}
	}

	outputTokens := util.EstimateTokens(finalThinking) + util.EstimateTokens(finalText)
	s.send("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
}

func recordClaudeStreamToolUse(tc util.ParsedToolCall, stopReason string, toolNames []string) {
	devcapture.Global().Record("claude_tool_use", "claude://messages/stream", "", 0, map[string]any{
		"name":        tc.Name,
		"input":       tc.Input,
		"stop_reason": stopReason,
		"tool_names":  toolNames,
	}, nil)
}

func (s *claudeStreamRuntime) prepareToolCallsForExecution(calls []util.ParsedToolCall) []util.ParsedToolCall {
	calls = util.NormalizeToolCallInputsForExecution(calls)
	calls = util.NormalizeParsedToolCallsForSchemas(calls, s.toolsRaw)
	var dropped bool
	calls, dropped = util.FilterParsedToolCallsByRequiredSchemasDetailed(calls, s.toolsRaw)
	if dropped {
		s.droppedInvalidTool = true
	}
	return calls
}

func (s *claudeStreamRuntime) onFinalize(reason streamengine.StopReason, scannerErr error) {
	if string(reason) == "upstream_error" {
		s.sendError(s.upstreamErr)
		return
	}
	if scannerErr != nil {
		s.sendError(scannerErr.Error())
		return
	}
	s.finalize("end_turn")
}
