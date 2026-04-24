package util

type StandardRequest struct {
	Surface        string
	RequestedModel string
	ResolvedModel  string
	ResponseModel  string
	ModelType      any
	Messages       []any
	FinalPrompt    string
	ToolNames      []string
	ToolChoice     ToolChoicePolicy
	Stream         bool
	Thinking       bool
	Search         bool
	PassThrough    map[string]any
}

type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceForced   ToolChoiceMode = "forced"
)

type ToolChoicePolicy struct {
	Mode       ToolChoiceMode
	ForcedName string
	Allowed    map[string]struct{}
}

func DefaultToolChoicePolicy() ToolChoicePolicy {
	return ToolChoicePolicy{Mode: ToolChoiceAuto}
}

func (p ToolChoicePolicy) IsNone() bool {
	return p.Mode == ToolChoiceNone
}

func (p ToolChoicePolicy) IsRequired() bool {
	return p.Mode == ToolChoiceRequired || p.Mode == ToolChoiceForced
}

func (p ToolChoicePolicy) Allows(name string) bool {
	if len(p.Allowed) == 0 {
		return true
	}
	_, ok := p.Allowed[name]
	return ok
}

func (r StandardRequest) CompletionPayload(sessionID string) map[string]any {
	payload := map[string]any{
		"chat_session_id":   sessionID,
		"parent_message_id": nil,
		"model_type":        r.ModelType,
		"prompt":            r.FinalPrompt,
		"ref_file_ids":      []any{},
		"thinking_enabled":  r.Thinking,
		"search_enabled":    r.Search,
	}
	for k, v := range r.PassThrough {
		payload[k] = v
	}
	return payload
}
