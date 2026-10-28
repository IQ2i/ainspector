package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingClient struct{ err error }

func (f failingClient) Complete(context.Context, Request) (Response, error) {
	return Response{Usage: Usage{Input: 7}}, f.err
}
func TestWithRedaction(t *testing.T) {
	const key = "sk-secret-0123456789"
	c := WithRedaction(failingClient{errors.New("401: Incorrect API key provided: " + key)}, key, "", "short")

	resp, err := c.Complete(context.Background(), Request{})
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("the key must be hidden, got %v", err)
	}
	if resp.Usage.Input != 7 {
		t.Errorf("the response and the backend must pass through, got %+v", resp)
	}
}

func TestWithRedaction_KeepsOtherErrorsAsTheyAre(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := WithRedaction(failingClient{sentinel}, "sk-secret-0123456789").Complete(context.Background(), Request{})
	if !errors.Is(err, sentinel) {
		t.Errorf("an error without secret must stay the same, got %v", err)
	}
}

func TestRedact_IgnoresShortSecrets(t *testing.T) {
	if got := Redact("a short word", "short"); got != "a short word" {
		t.Errorf("got %q", got)
	}
}
