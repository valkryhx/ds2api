package deepseek

import "testing"

func TestExtractSessionIDFromCreateSessionResponse_LegacyShape(t *testing.T) {
	resp := map[string]any{
		"data": map[string]any{
			"biz_data": map[string]any{
				"id": "legacy-session-id",
			},
		},
	}
	if got := extractSessionIDFromCreateSessionResponse(resp); got != "legacy-session-id" {
		t.Fatalf("expected legacy session id, got %q", got)
	}
}

func TestExtractSessionIDFromCreateSessionResponse_NewChatSessionShape(t *testing.T) {
	resp := map[string]any{
		"data": map[string]any{
			"biz_data": map[string]any{
				"chat_session": map[string]any{
					"id": "new-session-id",
				},
			},
		},
	}
	if got := extractSessionIDFromCreateSessionResponse(resp); got != "new-session-id" {
		t.Fatalf("expected new chat_session id, got %q", got)
	}
}

func TestExtractSessionIDFromCreateSessionResponse_MissingID(t *testing.T) {
	resp := map[string]any{
		"data": map[string]any{
			"biz_data": map[string]any{},
		},
	}
	if got := extractSessionIDFromCreateSessionResponse(resp); got != "" {
		t.Fatalf("expected empty session id, got %q", got)
	}
}

