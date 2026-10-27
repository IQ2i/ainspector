package llm

import (
	"context"
	"errors"
	"strings"
)

// minSecretLength avoids blanking out ordinary words when a secret is unexpectedly short
const minSecretLength = 8

// Redact replaces every secret found in s
func Redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= minSecretLength {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

type redactingClient struct {
	Client
	secrets []string
}

// WithRedaction hides the secrets from the errors of the client. Some endpoints echo the
// API key they rejected in their error body, and the error ends up in the log of the job.
func WithRedaction(c Client, secrets ...string) Client {
	return &redactingClient{Client: c, secrets: secrets}
}

func (r *redactingClient) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := r.Client.Complete(ctx, req)
	if err != nil {
		if msg := Redact(err.Error(), r.secrets...); msg != err.Error() {
			err = errors.New(msg)
		}
	}
	return resp, err
}
