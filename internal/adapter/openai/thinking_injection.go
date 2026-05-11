package openai

import (
	"strings"

	"ds2api/internal/util"
)

const (
	thinkingInjectionMarker        = "Reasoning Effort: Absolute maximum with no shortcuts permitted."
	defaultThinkingInjectionPrompt = thinkingInjectionMarker + "\n" +
		"You MUST be very thorough in your thinking and comprehensively decompose the problem to resolve the root cause, rigorously stress-testing your logic against all potential paths, edge cases, and adversarial scenarios.\n" +
		"Explicitly write out your entire deliberation process, documenting every intermediate step, considered alternative, and rejected hypothesis to ensure absolutely no assumption is left unchecked."
)

func appendThinkingInjectionPromptToLatestUser(messages []any, injectionPrompt string) ([]any, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	injectionPrompt = strings.TrimSpace(injectionPrompt)
	if injectionPrompt == "" {
		injectionPrompt = defaultThinkingInjectionPrompt
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(asString(msg["role"]))) != "user" {
			continue
		}
		content := msg["content"]
		normalizedContent := normalizeOpenAIContentForPrompt(content)
		if strings.Contains(normalizedContent, thinkingInjectionMarker) || strings.Contains(normalizedContent, injectionPrompt) {
			return messages, false
		}
		out := append([]any(nil), messages...)
		cloned := make(map[string]any, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = appendThinkingInjectionToContent(content, injectionPrompt)
		out[i] = cloned
		return out, true
	}
	return messages, false
}

func appendThinkingInjectionToContent(content any, injectionPrompt string) any {
	switch x := content.(type) {
	case string:
		return appendThinkingTextBlock(x, injectionPrompt)
	case []any:
		out := append([]any(nil), x...)
		out = append(out, map[string]any{
			"type": "text",
			"text": injectionPrompt,
		})
		return out
	default:
		return appendThinkingTextBlock(normalizeOpenAIContentForPrompt(content), injectionPrompt)
	}
}

func appendThinkingTextBlock(base, addition string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return addition
	}
	return base + "\n\n" + addition
}

func applyThinkingInjection(store ConfigReader, stdReq util.StandardRequest, traceID string) (util.StandardRequest, bool) {
	if store == nil || !store.ThinkingInjectionEnabled() || !stdReq.Thinking {
		return stdReq, false
	}
	messages, changed := appendThinkingInjectionPromptToLatestUser(stdReq.Messages, store.ThinkingInjectionPrompt())
	if !changed {
		return stdReq, false
	}
	finalPrompt, toolNames := buildOpenAIFinalPromptWithPolicy(messages, stdReq.ToolsRaw, traceID, stdReq.ToolChoice)
	if len(toolNames) == 0 && len(stdReq.ToolNames) > 0 {
		toolNames = stdReq.ToolNames
	}
	stdReq.Messages = messages
	stdReq.FinalPrompt = finalPrompt
	stdReq.PromptTokenText = finalPrompt
	stdReq.ToolNames = toolNames
	return stdReq, true
}
