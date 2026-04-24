package openai

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/util"

	"github.com/google/uuid"
)

const responsesFunctionArgsDeltaChunkSize = 128

func (s *responsesStreamRuntime) allocateOutputIndex() int {
	idx := s.nextOutputID
	s.nextOutputID++
	return idx
}

func (s *responsesStreamRuntime) ensureMessageItemID() string {
	if strings.TrimSpace(s.messageItemID) != "" {
		return s.messageItemID
	}
	s.messageItemID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.messageItemID
}

func (s *responsesStreamRuntime) ensureMessageOutputIndex() int {
	if s.messageOutputID >= 0 {
		return s.messageOutputID
	}
	s.messageOutputID = s.allocateOutputIndex()
	return s.messageOutputID
}

func (s *responsesStreamRuntime) ensureMessageItemAdded() {
	if s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.ensureMessageOutputIndex(), item),
	)
	s.messageAdded = true
}

func (s *responsesStreamRuntime) ensureMessageContentPartAdded() {
	if s.messagePartAdded {
		return
	}
	s.ensureMessageItemAdded()
	s.sendEvent(
		"response.content_part.added",
		openaifmt.BuildResponsesContentPartAddedPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			map[string]any{"type": "output_text", "text": ""},
		),
	)
	s.messagePartAdded = true
}

func (s *responsesStreamRuntime) emitTextDelta(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.ensureMessageContentPartAdded()
	s.visibleText.WriteString(content)
	s.sendEvent(
		"response.output_text.delta",
		openaifmt.BuildResponsesTextDeltaPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			content,
		),
	)
}

func (s *responsesStreamRuntime) closeMessageItem() {
	if !s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	outputIndex := s.ensureMessageOutputIndex()
	text := s.visibleText.String()
	if s.messagePartAdded {
		s.sendEvent(
			"response.output_text.done",
			openaifmt.BuildResponsesTextDonePayload(
				s.responseID,
				itemID,
				outputIndex,
				0,
				text,
			),
		)
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				itemID,
				outputIndex,
				0,
				map[string]any{"type": "output_text", "text": text},
			),
		)
		s.messagePartAdded = false
	}
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{
				"type": "output_text",
				"text": text,
			},
		},
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
	)
}

func (s *responsesStreamRuntime) ensureFunctionItemID(callIndex int) string {
	if id, ok := s.functionItemIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "fc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.functionItemIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) ensureToolCallID(callIndex int) string {
	if id, ok := s.streamToolCallIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.streamToolCallIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) ensureFunctionOutputIndex(callIndex int) int {
	if idx, ok := s.functionOutputIDs[callIndex]; ok {
		return idx
	}
	idx := s.allocateOutputIndex()
	s.functionOutputIDs[callIndex] = idx
	return idx
}

func (s *responsesStreamRuntime) ensureFunctionItemAdded(callIndex int, name string) {
	if strings.TrimSpace(name) != "" {
		s.functionNames[callIndex] = strings.TrimSpace(name)
	}
	if s.functionAdded[callIndex] {
		return
	}
	fnName := strings.TrimSpace(s.functionNames[callIndex])
	if fnName == "" {
		return
	}
	outputIndex := s.ensureFunctionOutputIndex(callIndex)
	itemID := s.ensureFunctionItemID(callIndex)
	callID := s.ensureToolCallID(callIndex)
	item := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      fnName,
		"arguments": "",
		"status":    "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, outputIndex, item),
	)
	s.functionAdded[callIndex] = true
	s.toolCallsEmitted = true
}

func (s *responsesStreamRuntime) emitFunctionCallDeltaEvents(deltas []toolCallDelta) {
	for _, d := range deltas {
		s.ensureFunctionItemAdded(d.Index, d.Name)
		if strings.TrimSpace(d.Arguments) == "" {
			continue
		}
		outputIndex := s.ensureFunctionOutputIndex(d.Index)
		itemID := s.ensureFunctionItemID(d.Index)
		callID := s.ensureToolCallID(d.Index)
		s.emitFunctionCallArgumentDeltaChunks(d.Index, itemID, outputIndex, callID, d.Arguments)
	}
}

func (s *responsesStreamRuntime) emitFunctionCallDoneEvents(calls []util.ParsedToolCall) {
	for idx, tc := range calls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		s.ensureFunctionItemAdded(idx, tc.Name)
		if s.functionDone[idx] {
			continue
		}
		outputIndex := s.ensureFunctionOutputIndex(idx)
		itemID := s.ensureFunctionItemID(idx)
		callID := s.ensureToolCallID(idx)
		argsBytes, _ := json.Marshal(tc.Input)
		args := string(argsBytes)
		s.emitMissingFunctionCallArgumentDeltas(idx, itemID, outputIndex, callID, args)
		s.functionArgs[idx] = args
		s.sendEvent(
			"response.function_call_arguments.done",
			openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(s.responseID, itemID, outputIndex, callID, tc.Name, args),
		)
		item := map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      tc.Name,
			"arguments": args,
			"status":    "completed",
		}
		s.sendEvent(
			"response.output_item.done",
			openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
		)
		s.functionDone[idx] = true
		s.toolCallsDoneEmitted = true
	}
}

func (s *responsesStreamRuntime) emitMissingFunctionCallArgumentDeltas(callIndex int, itemID string, outputIndex int, callID, args string) {
	existing := s.functionArgs[callIndex]
	switch {
	case existing == "":
		s.emitFunctionCallArgumentDeltaChunks(callIndex, itemID, outputIndex, callID, args)
	case strings.HasPrefix(args, existing):
		s.emitFunctionCallArgumentDeltaChunks(callIndex, itemID, outputIndex, callID, args[len(existing):])
	default:
		// If the canonicalized final args differ from streamed raw deltas,
		// keep prior deltas and only finalize with the normalized done payload.
	}
}

func (s *responsesStreamRuntime) emitFunctionCallArgumentDeltaChunks(callIndex int, itemID string, outputIndex int, callID, delta string) {
	if delta == "" {
		return
	}
	for _, chunk := range splitUTF8Chunks(delta, responsesFunctionArgsDeltaChunkSize) {
		if chunk == "" {
			continue
		}
		s.functionArgs[callIndex] += chunk
		s.sendEvent(
			"response.function_call_arguments.delta",
			openaifmt.BuildResponsesFunctionCallArgumentsDeltaPayload(s.responseID, itemID, outputIndex, callID, chunk),
		)
	}
}

func splitUTF8Chunks(s string, maxBytes int) []string {
	if s == "" {
		return nil
	}
	if maxBytes <= 0 || len(s) <= maxBytes {
		return []string{s}
	}
	out := make([]string, 0, (len(s)/maxBytes)+1)
	remaining := s
	for len(remaining) > 0 {
		if len(remaining) <= maxBytes {
			out = append(out, remaining)
			break
		}
		end := maxBytes
		for end > 0 && !utf8.RuneStart(remaining[end]) {
			end--
		}
		if end <= 0 {
			end = maxBytes
		}
		out = append(out, remaining[:end])
		remaining = remaining[end:]
	}
	return out
}
