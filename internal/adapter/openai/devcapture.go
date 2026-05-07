package openai

import (
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/devcapture"
	"ds2api/internal/util"
)

func recordOpenAIInbound(r *http.Request, a *auth.RequestAuth, req map[string]any) {
	if r == nil || req == nil {
		return
	}
	accountID := ""
	if a != nil {
		accountID = strings.TrimSpace(a.AccountID)
		if accountID == "" {
			accountID = strings.TrimSpace(a.CallerID)
		}
	}
	devcapture.Global().Record("openai_inbound", r.URL.String(), accountID, 0, map[string]any{
		"model":       req["model"],
		"stream":      req["stream"],
		"messages":    req["messages"],
		"input":       req["input"],
		"tools":       req["tools"],
		"tool_choice": req["tool_choice"],
		"max_tokens":  req["max_tokens"],
	}, nil)
}

func recordOpenAIToolUse(label, endpoint string, calls []util.ParsedToolCall, extra map[string]any) {
	if len(calls) == 0 {
		return
	}
	for _, tc := range calls {
		payload := map[string]any{
			"name":  tc.Name,
			"input": tc.Input,
		}
		for k, v := range extra {
			payload[k] = v
		}
		devcapture.Global().Record(label, endpoint, "", 0, payload, nil)
	}
}
