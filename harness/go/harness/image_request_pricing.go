package harness

import harnessllm "github.com/phodal/pero/harness/go/llm"

type ImageRequestPricingOptions = harnessllm.ImageRequestPricingOptions

type ImageRequestPrice = harnessllm.ImageRequestPrice
type ImageRequestPricing = harnessllm.ImageRequestPricing
type ImageRequestPricingFunc = harnessllm.ImageRequestPricingFunc
type ImageRequestPricingResolver = harnessllm.ImageRequestPricingResolver

// ImageRequestPricing resolves optional image pricing from one current route
// generation. Undeclared and unknown routes keep the fixed reference heuristic.
func (r *ProviderRegistry) ImageRequestPricing(provider, model string) (ImageRequestPricing, error) {
	if r == nil {
		return nil, nil
	}
	selected, err := r.route(provider)
	if err != nil {
		return nil, nil
	}
	resolver, ok := selected.adapter.(ImageRequestPricingResolver)
	if !ok {
		return nil, nil
	}
	return resolver.ImageRequestPricing(provider, model)
}

var _ ImageRequestPricingResolver = (*ProviderRegistry)(nil)
