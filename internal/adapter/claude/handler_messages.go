package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/devcapture"
	claudefmt "ds2api/internal/format/claude"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("anthropic-version")) == "" {
		r.Header.Set("anthropic-version", "2023-06-01")
	}
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeClaudeError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClaudeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	recordClaudeInboundRequest(r, a, req)
	norm, err := normalizeClaudeRequest(h.Store, req)
	if err != nil {
		writeClaudeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stdReq := norm.Standard

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, "invalid token.")
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, "Failed to get PoW")
		return
	}
	requestPayload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, requestPayload, pow, 3)
	if err != nil {
		writeClaudeError(w, http.StatusInternalServerError, "Failed to get Claude response.")
		return
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		writeClaudeError(w, http.StatusInternalServerError, string(body))
		return
	}

	if stdReq.Stream {
		h.handleClaudeStreamRealtime(w, r, resp, stdReq.ResponseModel, norm.NormalizedMessages, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolsRaw)
		return
	}
	result := sse.CollectStream(resp, stdReq.Thinking, true)
	textParsed := claudefmt.DetectClaudeToolCalls(result.Text, "", stdReq.ToolNames)
	thinkingParsed := claudefmt.DetectClaudeToolCalls("", result.Thinking, stdReq.ToolNames)
	config.Logger.Info(
		"[toolcall.debug] claude_nonstream_parse",
		"channel", "text+thinking",
		"tool_names", strings.Join(stdReq.ToolNames, ","),
		"text_rejected_tool_names", strings.Join(filteredRejectedToolNamesForLog(textParsed.RejectedToolNames), ","),
		"text_rejected_by_policy", textParsed.RejectedByPolicy,
		"text_saw_tool_syntax", textParsed.SawToolCallSyntax,
		"thinking_rejected_tool_names", strings.Join(filteredRejectedToolNamesForLog(thinkingParsed.RejectedToolNames), ","),
		"thinking_rejected_by_policy", thinkingParsed.RejectedByPolicy,
		"thinking_saw_tool_syntax", thinkingParsed.SawToolCallSyntax,
	)
	respBody := claudefmt.BuildMessageResponse(
		fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		stdReq.ResponseModel,
		norm.NormalizedMessages,
		result.Thinking,
		result.Text,
		stdReq.ToolNames,
		stdReq.ToolsRaw,
	)
	writeJSON(w, http.StatusOK, respBody)
}

func (h *Handler) handleClaudeStreamRealtime(w http.ResponseWriter, r *http.Request, resp *http.Response, model string, messages []any, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeClaudeError(w, http.StatusInternalServerError, string(body))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if !canFlush {
		config.Logger.Warn("[claude_stream] response writer does not support flush; streaming may be buffered")
	}

	streamRuntime := newClaudeStreamRuntime(
		w,
		rc,
		canFlush,
		model,
		messages,
		thinkingEnabled,
		searchEnabled,
		toolNames,
		toolsRaw,
	)
	streamRuntime.sendMessageStart()

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   claudeStreamPingInterval,
		IdleTimeout:         claudeStreamIdleTimeout,
		MaxKeepAliveNoInput: claudeStreamMaxKeepaliveCnt,
	}, streamengine.ConsumeHooks{
		OnKeepAlive: func() {
			streamRuntime.sendPing()
		},
		OnParsed:   streamRuntime.onParsed,
		OnFinalize: streamRuntime.onFinalize,
	})
}

func recordClaudeInboundRequest(r *http.Request, a *auth.RequestAuth, req map[string]any) {
	if r == nil || req == nil {
		return
	}
	accountID := ""
	if a != nil {
		accountID = a.AccountID
		if accountID == "" {
			accountID = a.CallerID
		}
	}
	devcapture.Global().Record("claude_inbound", r.URL.String(), accountID, 0, map[string]any{
		"model":          req["model"],
		"stream":         req["stream"],
		"system":         req["system"],
		"messages":       req["messages"],
		"tools":          req["tools"],
		"tool_choice":    req["tool_choice"],
		"max_tokens":     req["max_tokens"],
		"anthropic_beta": req["anthropic_beta"],
	}, nil)
}
