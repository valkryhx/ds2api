package openai

import (
	"encoding/json"
	"net/http"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

type chatStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	completionID string
	created      int64
	model        string
	finalPrompt  string
	toolNames    []string
	toolsRaw     any

	thinkingEnabled bool
	searchEnabled   bool
	upstreamErr     string
	upstreamErrCode string

	firstChunkSent       bool
	bufferToolContent    bool
	emitEarlyToolDeltas  bool
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	toolSieve         toolStreamSieveState
	streamToolCallIDs map[int]string
	streamToolNames   map[int]string
	thinking          strings.Builder
	text              strings.Builder
}

func newChatStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	completionID string,
	created int64,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	toolNames []string,
	bufferToolContent bool,
	emitEarlyToolDeltas bool,
	toolsRaw any,
) *chatStreamRuntime {
	return &chatStreamRuntime{
		w:                   w,
		rc:                  rc,
		canFlush:            canFlush,
		completionID:        completionID,
		created:             created,
		model:               model,
		finalPrompt:         finalPrompt,
		toolNames:           toolNames,
		toolsRaw:            toolsRaw,
		thinkingEnabled:     thinkingEnabled,
		searchEnabled:       searchEnabled,
		bufferToolContent:   bufferToolContent,
		emitEarlyToolDeltas: emitEarlyToolDeltas,
		streamToolCallIDs:   map[int]string{},
		streamToolNames:     map[int]string{},
	}
}

func (s *chatStreamRuntime) sendKeepAlive() {
	if !s.canFlush {
		return
	}
	_, _ = s.w.Write([]byte(": keep-alive\n\n"))
	_ = s.rc.Flush()
}

func (s *chatStreamRuntime) sendChunk(v any) {
	b, _ := json.Marshal(v)
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *chatStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *chatStreamRuntime) sendErrorChunk(message, code string) {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = "upstream_error"
	}
	s.sendChunk(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
			"code":    code,
			"param":   nil,
		},
	})
}

func (s *chatStreamRuntime) finalize(finishReason string) {
	if strings.TrimSpace(s.upstreamErr) != "" && (finishReason == "length" || finishReason == "content_filter") {
		s.sendErrorChunk(s.upstreamErr, s.upstreamErrCode)
	}
	finalThinking := s.thinking.String()
	finalText := s.text.String()
	detected := openaifmt.DetectChatToolCalls(finalText, finalThinking, s.permissiveParseToolNames())
	if len(detected.Calls) > 0 && !s.toolCallsDoneEmitted {
		finishReason = "tool_calls"
		delta := map[string]any{
			"tool_calls": formatFinalStreamToolCallsWithStableIDs(detected.Calls, s.toolNames, s.streamToolCallIDs, s.toolsRaw),
		}
		if !s.firstChunkSent {
			delta["role"] = "assistant"
			s.firstChunkSent = true
		}
		s.sendChunk(openaifmt.BuildChatStreamChunk(
			s.completionID,
			s.created,
			s.model,
			[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, delta)},
			nil,
		))
		s.toolCallsEmitted = true
		s.toolCallsDoneEmitted = true
	} else if s.bufferToolContent {
		for _, evt := range flushToolSieve(&s.toolSieve, s.permissiveParseToolNames()) {
			if len(evt.ToolCalls) > 0 {
				finishReason = "tool_calls"
				s.toolCallsEmitted = true
				s.toolCallsDoneEmitted = true
				tcDelta := map[string]any{
					"tool_calls": formatFinalStreamToolCallsWithStableIDs(evt.ToolCalls, s.toolNames, s.streamToolCallIDs, s.toolsRaw),
				}
				if !s.firstChunkSent {
					tcDelta["role"] = "assistant"
					s.firstChunkSent = true
				}
				s.sendChunk(openaifmt.BuildChatStreamChunk(
					s.completionID,
					s.created,
					s.model,
					[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, tcDelta)},
					nil,
				))
			}
			if evt.Content == "" {
				continue
			}
			delta := map[string]any{
				"content": evt.Content,
			}
			if !s.firstChunkSent {
				delta["role"] = "assistant"
				s.firstChunkSent = true
			}
			s.sendChunk(openaifmt.BuildChatStreamChunk(
				s.completionID,
				s.created,
				s.model,
				[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, delta)},
				nil,
			))
		}
	}

	if len(detected.Calls) > 0 || s.toolCallsEmitted {
		finishReason = "tool_calls"
	}
	recordOpenAIToolUse("openai_tool_use", "openai://chat/completions/stream", detected.Calls, map[string]any{
		"endpoint":         "/v1/chat/completions",
		"finish_reason":    finishReason,
		"thinking_enabled": s.thinkingEnabled,
		"model":            s.model,
	})
	s.sendChunk(openaifmt.BuildChatStreamChunk(
		s.completionID,
		s.created,
		s.model,
		[]map[string]any{openaifmt.BuildChatStreamFinishChoice(0, finishReason)},
		openaifmt.BuildChatUsage(s.finalPrompt, finalThinking, finalText),
	))
	s.sendDone()
}

func (s *chatStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ContentFilter || parsed.ErrorMessage != "" {
		if strings.TrimSpace(parsed.ErrorMessage) != "" {
			s.upstreamErr = parsed.ErrorMessage
			s.upstreamErrCode = parsed.ErrorCode
		}
		stopReason := streamengine.StopReason("content_filter")
		if strings.TrimSpace(parsed.ErrorCode) == "input_exceeds_limit" {
			stopReason = streamengine.StopReason("input_exceeds_limit")
		}
		return streamengine.ParsedDecision{Stop: true, StopReason: stopReason}
	}
	if parsed.Stop {
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
	}

	newChoices := make([]map[string]any, 0, len(parsed.Parts))
	contentSeen := false
	for _, p := range parsed.Parts {
		if s.searchEnabled && sse.IsCitation(p.Text) {
			continue
		}
		if p.Text == "" {
			continue
		}
		contentSeen = true
		delta := map[string]any{}
		if !s.firstChunkSent {
			delta["role"] = "assistant"
			s.firstChunkSent = true
		}
		if p.Type == "thinking" {
			s.thinking.WriteString(p.Text)
			if s.thinkingEnabled {
				delta["reasoning_content"] = p.Text
			}
		} else {
			s.text.WriteString(p.Text)
			if !s.bufferToolContent {
				delta["content"] = p.Text
			} else {
				events := processToolSieveChunk(&s.toolSieve, p.Text, s.permissiveParseToolNames())
				for _, evt := range events {
					if len(evt.ToolCallDeltas) > 0 {
						if !s.emitEarlyToolDeltas {
							continue
						}
						filtered := filterIncrementalToolCallDeltasByAllowed(evt.ToolCallDeltas, s.permissiveParseToolNames(), s.streamToolNames)
						if len(filtered) == 0 {
							continue
						}
						formatted := formatIncrementalStreamToolCallDeltas(filtered, s.streamToolCallIDs)
						if len(formatted) == 0 {
							continue
						}
						tcDelta := map[string]any{
							"tool_calls": formatted,
						}
						s.toolCallsEmitted = true
						if !s.firstChunkSent {
							tcDelta["role"] = "assistant"
							s.firstChunkSent = true
						}
						newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, tcDelta))
						continue
					}
					if len(evt.ToolCalls) > 0 {
						s.toolCallsEmitted = true
						s.toolCallsDoneEmitted = true
						tcDelta := map[string]any{
							"tool_calls": formatFinalStreamToolCallsWithStableIDs(evt.ToolCalls, s.toolNames, s.streamToolCallIDs, s.toolsRaw),
						}
						if !s.firstChunkSent {
							tcDelta["role"] = "assistant"
							s.firstChunkSent = true
						}
						newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, tcDelta))
						continue
					}
					if evt.Content != "" {
						contentDelta := map[string]any{
							"content": evt.Content,
						}
						if !s.firstChunkSent {
							contentDelta["role"] = "assistant"
							s.firstChunkSent = true
						}
						newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, contentDelta))
					}
				}
			}
		}
		if len(delta) > 0 {
			newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, delta))
		}
	}

	if len(newChoices) > 0 {
		s.sendChunk(openaifmt.BuildChatStreamChunk(s.completionID, s.created, s.model, newChoices, nil))
	}
	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}

func (s *chatStreamRuntime) permissiveParseToolNames() []string {
	return streamPermissiveToolNames(s.toolNames)
}
