package llm

import (
	"sort"
	"strings"
)

// Price is the cost of a model in USD per million tokens
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Pricing maps model names to their price
type Pricing map[string]Price

// defaultPrices are the Anthropic list prices. Other models have no built-in price: guessing
// the price of a third-party model would produce a wrong cost, so it is left to llm.prices.
var defaultPrices = Pricing{
	"claude-sonnet-5-5": {Input: 2, Output: 10, CacheRead: 0.20, CacheWrite: 2.50},
	"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20, CacheWrite: 5},
}

// NewPricing returns the built-in prices completed by overrides. A zero CacheRead or CacheWrite
// in an override means the usual 0.1x and 1.25x of the input price.
func NewPricing(overrides map[string]Price) Pricing {
	p := make(Pricing, len(defaultPrices)+len(overrides))
	for model, price := range defaultPrices {
		p[model] = price
	}
	for model, price := range overrides {
		if price.CacheRead == 0 {
			price.CacheRead = price.Input * 0.1
		}
		if price.CacheWrite == 0 {
			price.CacheWrite = price.Input * 1.25
		}
		p[model] = price
	}
	return p
}

// Cost estimates the cost in USD of a usage. ok is false when the model has no price.
// A dated model name (claude-sonnet-5-5-2026...) uses the price of its longest known prefix.
func (p Pricing) Cost(model string, u Usage) (cost float64, ok bool) {
	price, found := p[model]
	if !found {
		names := make([]string, 0, len(p))
		for name := range p {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
		for _, name := range names {
			if strings.HasPrefix(model, name) {
				price, found = p[name], true
				break
			}
		}
	}
	if !found {
		return 0, false
	}

	const perMillion = 1e6
	return (float64(u.Input)*price.Input +
		float64(u.Output)*price.Output +
		float64(u.CacheRead)*price.CacheRead +
		float64(u.CacheWrite)*price.CacheWrite) / perMillion, true
}
