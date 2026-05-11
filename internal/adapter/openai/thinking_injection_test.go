package openai

import (
	"strings"
	"testing"
)

func TestAppendThinkingInjectionToLatestUserStringContent(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "older"},
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := appendThinkingInjectionPromptToLatestUser(messages, "")
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	latest := out[2].(map[string]any)
	content, _ := latest["content"].(string)
	if !strings.Contains(content, "latest\n\n"+thinkingInjectionMarker) {
		t.Fatalf("expected injection after latest user text, got %q", content)
	}
	older := out[0].(map[string]any)
	if older["content"] != "older" {
		t.Fatalf("expected older user message unchanged, got %#v", older["content"])
	}
}

func TestAppendThinkingInjectionToLatestUserArrayContent(t *testing.T) {
	messages := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "latest"},
			},
		},
	}

	out, changed := appendThinkingInjectionPromptToLatestUser(messages, "")
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	content, _ := out[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected appended text block, got %#v", content)
	}
	block, _ := content[1].(map[string]any)
	if block["type"] != "text" || !strings.Contains(block["text"].(string), thinkingInjectionMarker) {
		t.Fatalf("unexpected appended block: %#v", block)
	}
}

func TestAppendThinkingInjectionToLatestUserSkipsDuplicate(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "latest\n\n" + defaultThinkingInjectionPrompt},
	}

	out, changed := appendThinkingInjectionPromptToLatestUser(messages, "")
	if changed {
		t.Fatal("expected duplicate injection to be skipped")
	}
	if len(out) != 1 {
		t.Fatalf("unexpected messages: %#v", out)
	}
}

func TestNormalizeOpenAIChatRequestAppliesThinkingInjection(t *testing.T) {
	cfg := mockOpenAIConfig{
		aliases:                  map[string]string{},
		wideInput:                true,
		thinkingInjectionEnabled: true,
		thinkingInjectionPrompt:  "custom thinking prompt",
		currentInputMinChars:     0,
	}
	req := map[string]any{
		"model": "deepseek-reasoner",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}

	out, err := normalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("normalizeOpenAIChatRequest error: %v", err)
	}
	if !strings.Contains(out.FinalPrompt, "hello\n\ncustom thinking prompt") {
		t.Fatalf("expected injected prompt in final prompt, got %q", out.FinalPrompt)
	}
	latest := out.Messages[0].(map[string]any)
	if latest["content"] != "hello\n\ncustom thinking prompt" {
		t.Fatalf("expected request messages updated, got %#v", latest["content"])
	}
}

func TestNormalizeOpenAIChatRequestSkipsThinkingInjectionWhenDisabled(t *testing.T) {
	cfg := mockOpenAIConfig{
		aliases:                  map[string]string{},
		wideInput:                true,
		thinkingInjectionEnabled: false,
		thinkingInjectionPrompt:  "custom thinking prompt",
	}
	req := map[string]any{
		"model": "deepseek-reasoner",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}

	out, err := normalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("normalizeOpenAIChatRequest error: %v", err)
	}
	if strings.Contains(out.FinalPrompt, "custom thinking prompt") {
		t.Fatalf("did not expect injected prompt when disabled, got %q", out.FinalPrompt)
	}
}
