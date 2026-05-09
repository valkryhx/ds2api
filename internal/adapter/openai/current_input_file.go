package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/util"
)

const (
	currentInputFilename    = "DS2API_HISTORY.txt"
	currentInputContentType = "text/plain; charset=utf-8"
	currentInputPurpose     = "assistants"
	currentInputLivePrompt  = "Continue from the latest state in the attached DS2API_HISTORY.txt context. Treat it as the current working state and answer the latest user request directly."
)

func (h *Handler) applyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq util.StandardRequest) (util.StandardRequest, error) {
	if h == nil || h.Store == nil || h.DS == nil || a == nil {
		return stdReq, nil
	}
	if stdReq.CurrentInputFileApplied || !h.Store.CurrentInputFileEnabled() {
		return stdReq, nil
	}
	_, latestUserText := latestUserInputForCurrentInputFile(stdReq.Messages)
	if strings.TrimSpace(latestUserText) == "" {
		return stdReq, nil
	}
	threshold := h.Store.CurrentInputFileMinChars()
	if len([]rune(latestUserText)) < threshold {
		return stdReq, nil
	}

	fileText := buildOpenAICurrentInputContextTranscript(stdReq.Messages, "")
	if strings.TrimSpace(fileText) == "" {
		return stdReq, errors.New("current user input file produced empty transcript")
	}
	modelType := "default"
	if resolvedType, ok := config.GetModelType(stdReq.ResolvedModel); ok {
		if s, ok := resolvedType.(string); ok && strings.TrimSpace(s) != "" {
			modelType = s
		}
	}
	result, err := h.DS.UploadFile(ctx, a, deepseek.UploadFileRequest{
		Filename:    currentInputFilename,
		ContentType: currentInputContentType,
		Purpose:     currentInputPurpose,
		ModelType:   modelType,
		Data:        []byte(fileText),
	}, 3)
	if err != nil {
		return stdReq, fmt.Errorf("upload current user input file: %w", err)
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return stdReq, errors.New("upload current user input file returned empty file id")
	}

	messages := []any{map[string]any{
		"role":    "user",
		"content": currentInputLivePrompt,
	}}
	stdReq.Messages = messages
	stdReq.HistoryText = fileText
	stdReq.CurrentInputFileApplied = true
	stdReq.RefFileIDs = prependUniqueRefFileID(stdReq.RefFileIDs, fileID)
	stdReq.FinalPrompt, stdReq.ToolNames = buildOpenAIFinalPromptWithPolicy(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice)
	if stdReq.ToolChoice.IsNone() && stdReq.Surface == "openai_chat" {
		stdReq.ToolNames = util.ToolChoiceNoneBlockParseNames()
	}
	stdReq.PromptTokenText = fileText + "\n" + stdReq.FinalPrompt
	return stdReq, nil
}

func mapCurrentInputFileError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	return http.StatusInternalServerError, err.Error()
}

func latestUserInputForCurrentInputFile(messages []any) (int, string) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		if role != "user" {
			continue
		}
		text := normalizeOpenAIContentForPrompt(msg["content"])
		if strings.TrimSpace(text) == "" {
			return -1, ""
		}
		return i, text
	}
	return -1, ""
}

func buildOpenAICurrentInputContextTranscript(messages []any, traceID string) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# DS2API_HISTORY.txt\n")
	b.WriteString("Prior conversation history and tool progress.\n\n")

	entry := 0
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := normalizeOpenAIRoleForPrompt(strings.ToLower(strings.TrimSpace(asString(msg["role"]))))
		content := strings.TrimSpace(buildOpenAIHistoryEntry(role, msg, traceID))
		if content == "" {
			continue
		}
		entry++
		fmt.Fprintf(&b, "=== %d. %s ===\n%s\n\n", entry, strings.ToUpper(roleLabelForHistory(role)), content)
	}
	transcript := strings.TrimSpace(b.String())
	if transcript == "" {
		return ""
	}
	return transcript + "\n"
}

func buildOpenAIHistoryEntry(role string, msg map[string]any, traceID string) string {
	switch role {
	case "assistant":
		content := normalizeOpenAIContentForPrompt(msg["content"])
		toolCalls := formatAssistantToolCallsForPrompt(msg, traceID)
		return strings.TrimSpace(joinNonEmpty(content, toolCalls))
	case "tool", "function":
		return strings.TrimSpace(buildToolHistoryContent(msg))
	case "system", "user":
		return strings.TrimSpace(normalizeOpenAIContentForPrompt(msg["content"]))
	default:
		return strings.TrimSpace(normalizeOpenAIContentForPrompt(msg["content"]))
	}
}

func buildToolHistoryContent(msg map[string]any) string {
	return formatToolResultForPrompt(msg)
}

func roleLabelForHistory(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "function":
		return "tool"
	case "":
		return "unknown"
	default:
		return role
	}
}

func prependUniqueRefFileID(existing []string, fileID string) []string {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return existing
	}
	out := make([]string, 0, len(existing)+1)
	out = append(out, fileID)
	for _, id := range existing {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || strings.EqualFold(trimmed, fileID) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
