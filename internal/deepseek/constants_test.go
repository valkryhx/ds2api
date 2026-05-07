package deepseek

import "testing"

func TestMaxKeepaliveCountRaisedForSlowFirstToken(t *testing.T) {
	if MaxKeepaliveCount != 40 {
		t.Fatalf("expected MaxKeepaliveCount=40 for slow first-token tolerance, got %d", MaxKeepaliveCount)
	}
}
