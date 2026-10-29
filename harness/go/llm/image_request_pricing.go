package llm

import harnessmodel "github.com/phodal/pero/harness/go/model"

// ImageRequestPrice describes the representation actually sent for one image
// occurrence. Offloaded images have zero visual tokens and placeholder text.
// Pricing is advisory; successfully reported provider usage remains authoritative.
type ImageRequestPrice struct {
	VisualTokens int    `json:"visualTokens"`
	Text         string `json:"text"`
}

// ImageRequestPricing prices a complete ordered batch of image occurrences.
// Implementations must return exactly one nonnegative price per input block.
// Offloaded is durable occurrence metadata, not a property of an attachment ID.
type ImageRequestPricing interface {
	PriceImages([]harnessmodel.ContentBlock, ImageRequestPricingOptions) ([]ImageRequestPrice, error)
}

type ImageRequestPricingFunc func([]harnessmodel.ContentBlock, ImageRequestPricingOptions) ([]ImageRequestPrice, error)

func (fn ImageRequestPricingFunc) PriceImages(images []harnessmodel.ContentBlock, options ImageRequestPricingOptions) ([]ImageRequestPrice, error) {
	return fn(images, options)
}

// ImageRequestPricingResolver is the optional adapter seam for request-specific
// image pricing. A nil result retains the route-independent fixed heuristic.
// A result must retain one complete adapter generation for its lifetime.
type ImageRequestPricingResolver interface {
	ImageRequestPricing(provider, model string) (ImageRequestPricing, error)
}

// ImageRequestPricingOptions resolves access in the same execution world as
// adapter dispatch. Empty paths preserve the no-local-path representation.
type ImageRequestPricingOptions struct {
	ResolveReadOnlyPath func(harnessmodel.ImageAttachmentRef) (string, error)
}
