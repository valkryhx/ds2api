package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/deepseek"
	"ds2api/internal/util"
)

type currentInputConfigStub struct {
	mockOpenAIConfig
	enabled  bool
	minChars int
}

func (c currentInputConfigStub) CurrentInputFileEnabled() bool { return c.enabled }
func (c currentInputConfigStub) CurrentInputFileMinChars() int { return c.minChars }

type currentInputDSStub struct {
	uploadReq         deepseek.UploadFileRequest
	uploadCalled      bool
	completionPayload map[string]any
	resp              *http.Response
}

func (d *currentInputDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-current-input", nil
}

func (d *currentInputDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow-current-input", nil
}

func (d *currentInputDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, req deepseek.UploadFileRequest, _ int) (*deepseek.UploadFileResult, error) {
	d.uploadCalled = true
	d.uploadReq = req
	return &deepseek.UploadFileResult{ID: "file-current-input-1", Status: "processed"}, nil
}

func (d *currentInputDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	d.completionPayload = payload
	if d.resp != nil {
		return d.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"ok\"}\ndata: [DONE]\n")),
	}, nil
}

func TestApplyCurrentInputFileUploadsTranscriptAndShrinksPrompt(t *testing.T) {
	h := &Handler{
		Store: currentInputConfigStub{mockOpenAIConfig: mockOpenAIConfig{wideInput: true}, enabled: true, minChars: 10},
		DS:    &currentInputDSStub{},
	}
	stdReq := util.StandardRequest{
		Surface:        "openai_chat",
		RequestedModel: "deepseek-chat",
		ResolvedModel:  "deepseek-chat",
		ResponseModel:  "deepseek-chat",
		ModelType:      nil,
		Messages: []any{
			map[string]any{"role": "system", "content": "You are concise."},
			map[string]any{"role": "user", "content": "first user turn with important context"},
			map[string]any{
				"role":    "assistant",
				"content": "I will call a tool.",
				"tool_calls": []any{
					map[string]any{
						"id": "call_1",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"file_path":"README.MD"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "name": "Read", "content": "file content"},
			map[string]any{"role": "user", "content": "latest user turn that is definitely long enough"},
		},
		ToolsRaw: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "Read",
					"parameters": map[string]any{
						"type": "object",
					},
				},
			},
		},
		FinalPrompt:     "original prompt includes first user turn with important context",
		PromptTokenText: "original prompt includes first user turn with important context",
		ToolNames:       []string{"Read"},
		ToolChoice:      util.DefaultToolChoicePolicy(),
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("applyCurrentInputFile returned error: %v", err)
	}
	ds := h.DS.(*currentInputDSStub)
	if !ds.uploadCalled {
		t.Fatal("expected UploadFile to be called")
	}
	if ds.uploadReq.Filename != "DS2API_HISTORY.txt" {
		t.Fatalf("unexpected upload filename: %q", ds.uploadReq.Filename)
	}
	if ds.uploadReq.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", ds.uploadReq.ContentType)
	}
	if ds.uploadReq.Purpose != "assistants" {
		t.Fatalf("unexpected purpose: %q", ds.uploadReq.Purpose)
	}
	uploaded := string(ds.uploadReq.Data)
	for _, want := range []string{
		"# DS2API_HISTORY.txt",
		"=== 1. SYSTEM ===",
		"=== 2. USER ===",
		"first user turn with important context",
		"[TOOL_CALL_HISTORY]",
		"[TOOL_RESULT_HISTORY]",
		"latest user turn that is definitely long enough",
	} {
		if !strings.Contains(uploaded, want) {
			t.Fatalf("expected uploaded transcript to contain %q, got:\n%s", want, uploaded)
		}
	}
	if !out.CurrentInputFileApplied {
		t.Fatal("expected current input file to be marked applied")
	}
	if len(out.RefFileIDs) != 1 || out.RefFileIDs[0] != "file-current-input-1" {
		t.Fatalf("unexpected ref file ids: %#v", out.RefFileIDs)
	}
	if strings.Contains(out.FinalPrompt, "first user turn with important context") {
		t.Fatalf("expected live prompt to omit full history, got %q", out.FinalPrompt)
	}
	if !strings.Contains(out.FinalPrompt, "Continue from the latest state in the attached DS2API_HISTORY.txt context.") {
		t.Fatalf("expected continuation prompt, got %q", out.FinalPrompt)
	}
	if !strings.Contains(out.PromptTokenText, "first user turn with important context") || !strings.Contains(out.PromptTokenText, out.FinalPrompt) {
		t.Fatalf("expected prompt token text to include transcript and live prompt, got %q", out.PromptTokenText)
	}
	payload := out.CompletionPayload("session-1")
	refIDs, _ := payload["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-current-input-1" {
		t.Fatalf("expected payload ref_file_ids to include upload id, got %#v", payload["ref_file_ids"])
	}
}

func TestChatCompletionsAppliesCurrentInputFileBeforeCompletion(t *testing.T) {
	ds := &currentInputDSStub{}
	h := &Handler{
		Store: currentInputConfigStub{mockOpenAIConfig: mockOpenAIConfig{wideInput: true}, enabled: true, minChars: 10},
		Auth:  streamStatusAuthStub{},
		DS:    ds,
	}
	reqBody := `{"model":"deepseek-chat","messages":[{"role":"user","content":"this is a long current request that should be uploaded"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !ds.uploadCalled {
		t.Fatal("expected UploadFile to be called")
	}
	if ds.completionPayload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	refIDs, _ := ds.completionPayload["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-current-input-1" {
		t.Fatalf("expected uploaded file id in completion payload, got %#v", ds.completionPayload["ref_file_ids"])
	}
	prompt, _ := ds.completionPayload["prompt"].(string)
	if strings.Contains(prompt, "this is a long current request") {
		t.Fatalf("expected completion prompt to be shortened, got %q", prompt)
	}
	if !strings.Contains(prompt, "DS2API_HISTORY.txt") {
		t.Fatalf("expected continuation prompt to mention DS2API_HISTORY.txt, got %q", prompt)
	}
}
