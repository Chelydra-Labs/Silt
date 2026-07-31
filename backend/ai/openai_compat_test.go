package ai

import (
	"net/http"
	"testing"
)

func TestOpenAIClassifyError_RateLimitProse(t *testing.T) {
	raw := []byte(`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`)
	e := openaiClassifyError(raw, http.StatusTooManyRequests)
	if e == nil {
		t.Fatal("expected AIError")
	}
	if e.Kind != ErrRateLimited {
		t.Errorf("kind = %q, want rate-limited", e.Kind)
	}
	if e.Message == "" || e.Message[0] == '{' {
		t.Errorf("message should be prose, got %q", e.Message)
	}
	if e.Status != 429 {
		t.Errorf("status = %d", e.Status)
	}
}

func TestOpenAIClassifyError_NonJSONReturnsNil(t *testing.T) {
	if e := openaiClassifyError([]byte("not json"), 429); e != nil {
		t.Errorf("want nil, got %+v", e)
	}
}
