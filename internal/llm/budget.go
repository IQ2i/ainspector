package llm

import (
	"context"
	"errors"
	"sync"
)

// ErrBudgetExceeded is returned once a run has consumed its token budget
var ErrBudgetExceeded = errors.New("token budget exceeded")

// Logger receives one line per event worth keeping in the job log
type Logger func(format string, args ...any)

// Totals is what a run consumed
type Totals struct {
	Usage Usage
	Calls int
	// Cost is the estimate in USD of the calls whose model has a price
	Cost float64
	// UnpricedCalls counts the calls whose model has no price: Cost is then incomplete
	UnpricedCalls int
}

// Sub returns what was consumed between an earlier snapshot and t
func (t Totals) Sub(earlier Totals) Totals {
	return Totals{
		Usage: Usage{
			Input:      t.Usage.Input - earlier.Usage.Input,
			Output:     t.Usage.Output - earlier.Usage.Output,
			CacheRead:  t.Usage.CacheRead - earlier.Usage.CacheRead,
			CacheWrite: t.Usage.CacheWrite - earlier.Usage.CacheWrite,
		},
		Calls:         t.Calls - earlier.Calls,
		Cost:          t.Cost - earlier.Cost,
		UnpricedCalls: t.UnpricedCalls - earlier.UnpricedCalls,
	}
}

// Budget wraps a Client to cap the tokens of a run and account for what it consumed.
// It is safe for concurrent use. The cap is soft: calls already in flight when it is
// reached still complete.
type Budget struct {
	inner   Client
	max     int
	pricing Pricing
	log     Logger

	mu       sync.Mutex
	used     int
	totals   Totals
	reported bool
}

// WithBudget caps the tokens consumed through c at maxTokens. Tokens read from the cache are
// not counted: they cost a fraction of fresh ones, and a long shared prefix would exhaust
// the budget long before the money.
func WithBudget(c Client, maxTokens int, pricing Pricing, log Logger) *Budget {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Budget{inner: c, max: maxTokens, pricing: pricing, log: log}
}

func (b *Budget) Backend() string { return b.inner.Backend() }

// Complete forwards the request unless the budget is spent
func (b *Budget) Complete(ctx context.Context, req Request) (Response, error) {
	b.mu.Lock()
	if b.used >= b.max {
		first := !b.reported
		b.reported = true
		b.mu.Unlock()
		if first {
			b.log("Token budget of %d reached, the remaining units will not be reviewed", b.max)
		}
		return Response{}, ErrBudgetExceeded
	}
	b.mu.Unlock()

	resp, err := b.inner.Complete(ctx, req)
	// A failed call may still have been billed: backends return what they consumed with the error
	if err == nil || resp.Usage != (Usage{}) {
		b.account(resp)
	}
	return resp, err
}

func (b *Budget) account(resp Response) {
	attempts := resp.Attempts
	if len(attempts) == 0 {
		attempts = []Attempt{{Model: resp.Model, Usage: resp.Usage}}
	}

	var cost float64
	unpriced := 0
	for _, a := range attempts {
		if c, ok := b.pricing.Cost(a.Model, a.Usage); ok {
			cost += c
		} else {
			unpriced++
		}
	}

	b.mu.Lock()
	b.used += resp.Usage.Input + resp.Usage.Output + resp.Usage.CacheWrite
	b.totals.Calls++
	b.totals.Usage.Input += resp.Usage.Input
	b.totals.Usage.Output += resp.Usage.Output
	b.totals.Usage.CacheRead += resp.Usage.CacheRead
	b.totals.Usage.CacheWrite += resp.Usage.CacheWrite
	b.totals.Cost += cost
	b.totals.UnpricedCalls += unpriced
	used := b.used
	b.mu.Unlock()

	b.log("LLM call (%s): %d in, %d out, %d cache read, %d cache write; budget %d/%d", resp.Model,
		resp.Usage.Input, resp.Usage.Output, resp.Usage.CacheRead, resp.Usage.CacheWrite, used, b.max)
}

// Totals returns what was consumed so far
func (b *Budget) Totals() Totals {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totals
}
