package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

type fakeClient struct {
	mu    sync.Mutex
	calls int
	resp  Response
	err   error
}

func (f *fakeClient) Complete(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.resp, f.err
}

func (f *fakeClient) Backend() string { return "fake" }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPricing_Cost(t *testing.T) {
	p := NewPricing(map[string]Price{"custom": {Input: 1, Output: 4}})
	usage := Usage{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}

	tests := []struct {
		model string
		want  float64
		ok    bool
	}{
		{"claude-sonnet-5-5", 2 + 10 + 0.20 + 2.50, true},
		{"claude-opus-5-5", 4 + 20 + 0.20 + 5, true},
		{"claude-opus-5-5-20261001", 4 + 20 + 0.20 + 5, true},
		{"custom", 1 + 4 + 0.1 + 1.25, true},
		{"gpt-4o", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got, ok := p.Cost(tt.model, usage)
			if ok != tt.ok || !near(got, tt.want) {
				t.Errorf("expected %v/%v, got %v/%v", tt.want, tt.ok, got, ok)
			}
		})
	}
}

func TestPricing_OverrideReplacesBuiltIn(t *testing.T) {
	p := NewPricing(map[string]Price{"claude-sonnet-5-5": {Input: 1, Output: 1, CacheRead: 0.5, CacheWrite: 2}})
	got, _ := p.Cost("claude-sonnet-5-5", Usage{Input: 1_000_000})
	if !near(got, 1) {
		t.Errorf("expected the override to apply, got %v", got)
	}
}

func TestBudget_AccumulatesAndForwards(t *testing.T) {
	inner := &fakeClient{resp: Response{Model: "claude-sonnet-5-5", Text: "ok", Usage: Usage{Input: 100, Output: 50, CacheRead: 900, CacheWrite: 10}}}
	var logs []string
	b := WithBudget(inner, 1000, NewPricing(nil), func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })

	for i := 0; i < 2; i++ {
		resp, err := b.Complete(context.Background(), Request{})
		if err != nil || resp.Text != "ok" {
			t.Fatalf("unexpected result %+v / %v", resp, err)
		}
	}

	totals := b.Totals()
	if totals.Calls != 2 || totals.Usage != (Usage{Input: 200, Output: 100, CacheRead: 1800, CacheWrite: 20}) {
		t.Errorf("unexpected totals %+v", totals)
	}
	wantCost := 2 * (100*2.0 + 50*10.0 + 900*0.20 + 10*2.50) / 1e6
	if !near(totals.Cost, wantCost) || totals.UnpricedCalls != 0 {
		t.Errorf("expected cost %v, got %+v", wantCost, totals)
	}
	if b.Backend() != "fake" || len(logs) != 2 {
		t.Errorf("unexpected backend or logs: %s %v", b.Backend(), logs)
	}
}

func TestBudget_StopsWhenSpent(t *testing.T) {
	inner := &fakeClient{resp: Response{Model: "m", Usage: Usage{Input: 60, Output: 40}}}
	var logs []string
	b := WithBudget(inner, 150, nil, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })

	var errs []error
	for i := 0; i < 4; i++ {
		_, err := b.Complete(context.Background(), Request{})
		errs = append(errs, err)
	}

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("the first two calls fit the budget, got %v", errs)
	}
	if !errors.Is(errs[2], ErrBudgetExceeded) || !errors.Is(errs[3], ErrBudgetExceeded) {
		t.Errorf("expected ErrBudgetExceeded once spent, got %v", errs)
	}
	if inner.calls != 2 {
		t.Errorf("a refused call must not reach the backend, got %d calls", inner.calls)
	}
	exceeded := 0
	for _, l := range logs {
		if l == "Token budget of 150 reached, the remaining units will not be reviewed" {
			exceeded++
		}
	}
	if exceeded != 1 {
		t.Errorf("the exhaustion must be logged once, got %d in %v", exceeded, logs)
	}
}

func TestBudget_CacheReadsAreFree(t *testing.T) {
	inner := &fakeClient{resp: Response{Model: "m", Usage: Usage{Input: 10, CacheRead: 100000}}}
	b := WithBudget(inner, 100, nil, nil)
	for i := 0; i < 5; i++ {
		if _, err := b.Complete(context.Background(), Request{}); err != nil {
			t.Fatalf("cache reads must not consume the budget: %v", err)
		}
	}
}

func TestBudget_BackendErrorIsNotCounted(t *testing.T) {
	boom := errors.New("boom")
	b := WithBudget(&fakeClient{err: boom}, 100, nil, nil)
	if _, err := b.Complete(context.Background(), Request{}); !errors.Is(err, boom) {
		t.Fatalf("expected the backend error, got %v", err)
	}
	if got := b.Totals(); got.Calls != 0 {
		t.Errorf("a failed call must not be counted, got %+v", got)
	}
}

func TestBudget_UnpricedModel(t *testing.T) {
	b := WithBudget(&fakeClient{resp: Response{Model: "gpt-4o", Usage: Usage{Input: 5}}}, 100, NewPricing(nil), nil)
	_, _ = b.Complete(context.Background(), Request{})
	if got := b.Totals(); got.UnpricedCalls != 1 || got.Cost != 0 {
		t.Errorf("expected one unpriced call, got %+v", got)
	}
}

func TestBudget_Concurrent(t *testing.T) {
	inner := &fakeClient{resp: Response{Model: "claude-sonnet-5-5", Usage: Usage{Input: 1, Output: 1}}}
	b := WithBudget(inner, 1_000_000, NewPricing(nil), nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Complete(context.Background(), Request{})
		}()
	}
	wg.Wait()

	if got := b.Totals(); got.Calls != 50 || got.Usage.Input != 50 {
		t.Errorf("unexpected totals %+v", got)
	}
}

func TestBudget_BilledFailuresExhaustTheBudget(t *testing.T) {
	boom := errors.New("truncated")
	inner := &fakeClient{resp: Response{Model: "claude-sonnet-5-5", Usage: Usage{Input: 60, Output: 40}}, err: boom}
	b := WithBudget(inner, 150, NewPricing(nil), nil)

	for i := 0; i < 2; i++ {
		if _, err := b.Complete(context.Background(), Request{}); !errors.Is(err, boom) {
			t.Fatalf("expected the backend error, got %v", err)
		}
	}
	if _, err := b.Complete(context.Background(), Request{}); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("failed but billed calls must count against the budget, got %v", err)
	}
	if got := b.Totals(); got.Calls != 2 || got.Usage.Output != 80 || got.Cost == 0 {
		t.Errorf("expected the billed failures in the totals, got %+v", got)
	}
}

func TestBudget_PricesEachAttempt(t *testing.T) {
	inner := &fakeClient{resp: Response{
		Model: "claude-opus-5-5",
		Usage: Usage{Input: 2000000, Output: 0},
		Attempts: []Attempt{
			{Model: "claude-sonnet-5-5", Usage: Usage{Input: 1000000}},
			{Model: "claude-opus-5-5", Usage: Usage{Input: 1000000}},
			{Model: "other-model", Usage: Usage{Input: 5}},
		},
	}}
	b := WithBudget(inner, 10000000, NewPricing(nil), nil)
	if _, err := b.Complete(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}

	got := b.Totals()
	if !near(got.Cost, 2+4) || got.UnpricedCalls != 1 || got.Calls != 1 {
		t.Errorf("expected each attempt at the price of its model, got %+v", got)
	}
}

func TestTotals_Sub(t *testing.T) {
	later := Totals{Usage: Usage{Input: 10, Output: 5, CacheRead: 8, CacheWrite: 3}, Calls: 4, Cost: 0.5, UnpricedCalls: 2}
	earlier := Totals{Usage: Usage{Input: 4, Output: 1, CacheRead: 2, CacheWrite: 3}, Calls: 1, Cost: 0.2, UnpricedCalls: 1}

	got := later.Sub(earlier)

	want := Totals{Usage: Usage{Input: 6, Output: 4, CacheRead: 6}, Calls: 3, Cost: 0.3, UnpricedCalls: 1}
	if got.Usage != want.Usage || got.Calls != want.Calls || got.UnpricedCalls != want.UnpricedCalls || !near(got.Cost, want.Cost) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}
