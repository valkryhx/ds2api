package util

import "testing"

func TestHasMalformedToolCallFragmentDetectsDanglingCDATA(t *testing.T) {
	text := `<|DSML|tool_calls><|DSML|invoke name="Bash"><|DSML|parameter name="command"><![CDATA[git -C D:/git_repos/ds2api log --oneline -10`
	if !HasMalformedToolCallFragment(text) {
		t.Fatalf("expected malformed fragment to be detected")
	}
}

func TestHasMalformedToolCallFragmentDetectsUnmatchedToolCallsWrapper(t *testing.T) {
	text := `<tool_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke>`
	if !HasMalformedToolCallFragment(text) {
		t.Fatalf("expected unmatched tool_calls wrapper to be detected")
	}
}

func TestHasMalformedToolCallFragmentAllowsCompleteDSML(t *testing.T) {
	text := `<|DSML|tool_calls><|DSML|invoke name="Bash"><|DSML|parameter name="command"><![CDATA[pwd]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`
	if HasMalformedToolCallFragment(text) {
		t.Fatalf("expected complete DSML fragment to be treated as valid")
	}
}
