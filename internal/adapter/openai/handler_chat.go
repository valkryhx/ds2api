package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if isVercelStreamReleaseRequest(r) {
		h.handleVercelStreamRelease(w, r)
		return
	}
	if isVercelStreamPrepareRequest(r) {
		h.handleVercelStreamPrepare(w, r)
		return
	}

	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	stdReq, err := normalizeOpenAIChatRequest(h.Store, req, requestTraceID(r))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		if a.UseConfigToken {
			writeOpenAIError(w, http.StatusUnauthorized, "Account token is invalid. Please re-login the account in admin.")
		} else {
			writeOpenAIError(w, http.StatusUnauthorized, "Invalid token. If this should be a DS2API key, add it to config.keys first.")
		}
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, "Failed to get PoW (invalid token or unknown error).")
		return
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, payload, pow, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "Failed to get completion.")
		return
	}
	if stdReq.Stream {
		h.handleStream(w, r, resp, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames)
		return
	}
	h.handleNonStream(w, r.Context(), resp, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.ToolNames)
}

func (h *Handler) handleNonStream(w http.ResponseWriter, ctx context.Context, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled bool, toolNames []string) {
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	_ = ctx
	result := sse.CollectStream(resp, thinkingEnabled, true)

	finalThinking := result.Thinking
	finalText := result.Text
	// Chat/completions tool parsing should remain permissive: if the upstream model
	// emits a valid standalone tool payload, pass it through even when the request's
	// declared tool list is partial.
	parseToolNames := toolNames[:0]
	respBody := openaifmt.BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText, parseToolNames)
	writeJSON(w, http.StatusOK, respBody)
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if !canFlush {
		config.Logger.Warn("[stream] response writer does not support flush; streaming may be buffered")
	}

	created := time.Now().Unix()
	bufferToolContent := h.toolcallFeatureMatchEnabled()
	emitEarlyToolDeltas := h.toolcallEarlyEmitHighConfidence()
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	// Keep chat/completions tool interception permissive (do not hard-filter by
	// declared tools in the request payload).
	parseToolNames := toolNames[:0]

	streamRuntime := newChatStreamRuntime(
		w,
		rc,
		canFlush,
		completionID,
		created,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		parseToolNames,
		bufferToolContent,
		emitEarlyToolDeltas,
	)

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(deepseek.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(deepseek.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: deepseek.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnKeepAlive: func() {
			streamRuntime.sendKeepAlive()
		},
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if string(reason) == "content_filter" {
				streamRuntime.finalize("content_filter")
				return
			}
			streamRuntime.finalize("stop")
		},
	})
}
