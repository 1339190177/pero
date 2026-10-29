package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	harnessmedia "github.com/phodal/pero/harness/go/media"
	harnessmodel "github.com/phodal/pero/harness/go/model"
	harnesssession "github.com/phodal/pero/harness/go/session"
)

const (
	tokenCharactersPerToken = 4
	tokenBlockOverhead      = 4
	tokenRoleOverhead       = 4
)

type TokenNode struct {
	Seq             uint64  `json:"seq"`
	Tokens          int     `json:"tokens"`
	HeuristicTokens int     `json:"heuristicTokens"`
	Message         Message `json:"message"`
}

type TokenMeasurement struct {
	TotalTokens   int         `json:"totalTokens"`
	SurfaceTokens int         `json:"surfaceTokens"`
	HeaderTokens  int         `json:"headerTokens"`
	Baseline      string      `json:"baseline"`
	Nodes         []TokenNode `json:"nodes"`
}

// TokenMeter applies the same fixed-density estimator as DSH and anchors the
// result to the latest provider-reported usage when that is more conservative.
type TokenMeter struct {
	// ImagePricing resolves the same optional route projection used for dispatch.
	// Nil preserves the fixed reference heuristic, including offloaded images.
	ImagePricing ImageRequestPricingResolver
	Attachments  AttachmentStore
}

func (TokenMeter) EstimateMessageTokens(message Message) int { return EstimateMessageTokens(message) }

func (meter TokenMeter) Measure(session *Session) (TokenMeasurement, error) {
	if session == nil {
		return TokenMeasurement{}, fmt.Errorf("harness: token meter requires a session")
	}
	var header RequestHeaderEvent
	hasHeader := false
	canonicalSystem := false
	measurement := TokenMeasurement{Baseline: "estimated"}
	events := session.Events()
	surface := make([]uint64, 0)
	var anchorHeader *RequestHeaderEvent
	var anchorSurface []uint64
	var anchorUsage *TokenUsage
	var anchorEventCount int
	for eventIndex, event := range events {
		if event.Type == "system/message" {
			canonicalSystem = true
			header.System = ""
		}
		if event.Type == "request/header" {
			var next RequestHeaderEvent
			if err := json.Unmarshal(event.Data, &next); err != nil {
				return TokenMeasurement{}, fmt.Errorf("harness: decode request/header at seq %d: %w", event.Seq, err)
			}
			header = next
			hasHeader = true
			if canonicalSystem {
				header.System = ""
			}
		}
		var applyErr error
		surface, applyErr = projectSurface(surface, event)
		if applyErr != nil {
			return TokenMeasurement{}, applyErr
		}
		if event.Type != "assistant/message" {
			continue
		}
		var payload struct {
			Usage *TokenUsage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return TokenMeasurement{}, fmt.Errorf("harness: decode assistant usage at seq %d: %w", event.Seq, err)
		}
		// Only the latest completed call can anchor pressure. A call without
		// usage invalidates an older provider baseline.
		anchorHeader, anchorUsage = nil, nil
		if payload.Usage != nil && hasHeader {
			copyHeader := header
			copyUsage := *payload.Usage
			anchorHeader = &copyHeader
			anchorUsage = &copyUsage
			anchorSurface = append(anchorSurface[:0], surface...)
			anchorEventCount = eventIndex + 1
		}
	}
	measurement.HeaderTokens = 0
	if hasHeader {
		measurement.HeaderTokens = estimateRequestHeader(header)
	}
	plan, err := harnesssession.BuildContextPlan(events, surface)
	if err != nil {
		return TokenMeasurement{}, err
	}
	var pricing ImageRequestPricing
	if meter.ImagePricing != nil && hasHeader {
		pricing, err = meter.ImagePricing.ImageRequestPricing(header.Provider, header.Model)
		if err != nil {
			return TokenMeasurement{}, err
		}
	}
	nodes := make([]TokenNode, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		nodes = append(nodes, TokenNode{Seq: node.Seq, Tokens: node.Tokens, Message: node.Message})
	}
	measurement.Nodes, measurement.SurfaceTokens, err = priceTokenNodesWithFiles(nodes, pricing, meter.Attachments, meter.imagePricingOptions())
	if err != nil {
		return TokenMeasurement{}, err
	}

	estimated := measurement.HeaderTokens + measurement.SurfaceTokens
	measurement.TotalTokens = estimated
	if anchorHeader != nil && anchorUsage != nil && requestEnvelopeEqual(*anchorHeader, header) {
		anchorPlan, err := harnesssession.BuildContextPlan(events[:anchorEventCount], anchorSurface)
		if err != nil {
			return TokenMeasurement{}, err
		}
		anchorNodes := make([]TokenNode, 0, len(anchorPlan.Nodes))
		for _, node := range anchorPlan.Nodes {
			anchorNodes = append(anchorNodes, TokenNode{Seq: node.Seq, Tokens: node.Tokens, Message: node.Message})
		}
		_, anchorSurfaceTokens, err := priceTokenNodesWithFiles(anchorNodes, pricing, meter.Attachments, meter.imagePricingOptions())
		if err != nil {
			return TokenMeasurement{}, err
		}
		estimatedAnchor := estimateRequestHeader(*anchorHeader) + anchorSurfaceTokens
		usageTokens := observedUsageTokens(*anchorUsage)
		if usageTokens >= estimatedAnchor {
			measurement.TotalTokens = max(0, usageTokens+measurement.SurfaceTokens-anchorSurfaceTokens)
			measurement.Baseline = "usage"
		}
	}
	if len(surface) == 0 && !hasHeader {
		measurement.Baseline = "none"
	}
	return measurement, nil
}

func projectSurface(surface []uint64, event Event) ([]uint64, error) {
	if !isSurfaceEventType(event.Type) {
		return surface, nil
	}
	if len(event.SurfaceOp) == 0 || string(event.SurfaceOp) == `"append"` {
		return append(surface, event.Seq), nil
	}
	var operation struct {
		Op    string `json:"op"`
		Start uint64 `json:"start"`
		End   uint64 `json:"end"`
	}
	if err := json.Unmarshal(event.SurfaceOp, &operation); err != nil || operation.Op != "replace" {
		return nil, fmt.Errorf("harness: invalid surface operation at seq %d", event.Seq)
	}
	start, end := -1, -1
	for index, seq := range surface {
		if seq == operation.Start && start < 0 {
			start = index
		}
		if seq == operation.End {
			end = index
		}
	}
	if start < 0 || end < start {
		return nil, fmt.Errorf("harness: replacement at seq %d names a missing or reversed surface range %d-%d", event.Seq, operation.Start, operation.End)
	}
	next := make([]uint64, 0, len(surface)-(end-start))
	next = append(next, surface[:start]...)
	next = append(next, event.Seq)
	next = append(next, surface[end+1:]...)
	return next, nil
}

// DSH's surface projection retains empty assistant events for usage and
// replacement identity but derives no model-visible message from them.
func surfaceMessageTokens(event Event, message Message) int {
	if (event.Type == "assistant/message" || event.Type == "system/message") && len(message.Content) == 0 {
		return 0
	}
	return EstimateMessageTokens(message)
}

func requestEnvelopeEqual(left, right RequestHeaderEvent) bool {
	return left.Provider == right.Provider && left.Model == right.Model && left.ReasoningEffort == right.ReasoningEffort &&
		reflect.DeepEqual(left.Temperature, right.Temperature) && left.MaxTokens == right.MaxTokens &&
		reflect.DeepEqual(left.Stop, right.Stop) && left.System == right.System && reflect.DeepEqual(left.Tools, right.Tools)
}

func observedUsageTokens(usage TokenUsage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
}

func estimateRequestHeader(header RequestHeaderEvent) int {
	tokens := divideTokens(stringCodeUnits(header.System))
	if header.System != "" {
		tokens += tokenRoleOverhead
	}
	if len(header.Tools) > 0 {
		// Price the exact canonical DSH envelope, not Go's inputSchema field
		// spelling or JSON's optional HTML escaping.
		type tool struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			Parameters   json.RawMessage `json:"parameters"`
			DeferLoading bool            `json:"deferLoading,omitempty"`
		}
		tools := make([]tool, 0, len(header.Tools))
		for _, schema := range header.Tools {
			raw := schema.InputSchema
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			tools = append(tools, tool{schema.Name, schema.Description, raw, schema.DeferLoading})
		}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(tools); err == nil {
			tokens += divideTokens(stringCodeUnits(string(bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))))) + tokenBlockOverhead
		}
	}
	return tokens
}

func EstimateMessageTokens(message Message) int {
	return harnessmodel.EstimateMessageTokens(message)
}

// stringCodeUnits mirrors JavaScript String.length so DSH and Go price
// non-ASCII content under the same UTF-16 density without allocating.
func stringCodeUnits(value string) int {
	units := 0
	for _, character := range value {
		units++
		if character > 0xffff {
			units++
		}
	}
	return units
}

func divideTokens(characters int) int {
	return (characters + tokenCharactersPerToken - 1) / tokenCharactersPerToken
}

// priceTokenNodes replaces attachment-reference structural costs. The provider
// receives a detached, complete batch in request order, and the fixed heuristic
// remains available for context-breakdown and replacement shadow prices.
func priceTokenNodes(nodes []TokenNode, pricing ImageRequestPricing, options ...ImageRequestPricingOptions) ([]TokenNode, int, error) {
	return priceTokenNodesWithFiles(nodes, pricing, nil, options...)
}

func priceTokenNodesWithFiles(nodes []TokenNode, pricing ImageRequestPricing, attachments AttachmentStore, options ...ImageRequestPricingOptions) ([]TokenNode, int, error) {
	images := make([]ContentBlock, 0)
	counts := make([]int, len(nodes))
	structural := make([]int, len(nodes))
	fileDelta := make([]int, len(nodes))
	for index := range nodes {
		node := &nodes[index]
		node.HeuristicTokens = node.Tokens
		if node.Tokens != 0 {
			projected, err := harnessmedia.ProjectFilesToText([]Message{node.Message}, attachments)
			if err != nil {
				return nil, 0, err
			}
			fileDelta[index] = EstimateMessageTokens(projected[0]) - EstimateMessageTokens(node.Message)
		}
		if pricing == nil || node.Tokens == 0 {
			continue
		}
		before := len(images)
		withoutImages := node.Message
		withoutImages.Content = tokenContentWithoutImages(node.Message.Content, &images)
		counts[index] = len(images) - before
		structural[index] = EstimateMessageTokens(node.Message) - EstimateMessageTokens(withoutImages)
	}
	var prices []ImageRequestPrice
	if len(images) > 0 {
		var err error
		var access ImageRequestPricingOptions
		if len(options) > 0 {
			access = options[0]
		}
		prices, err = pricing.PriceImages(images, access)
		if err != nil {
			return nil, 0, err
		}
		if len(prices) != len(images) {
			return nil, 0, fmt.Errorf("harness: route image pricing answered %d prices for %d occurrences", len(prices), len(images))
		}
	}
	cursor, total := 0, 0
	maxInt := int(^uint(0) >> 1)
	for index := range nodes {
		node := &nodes[index]
		node.Tokens += fileDelta[index]
		node.Tokens -= structural[index]
		for occurrence := 0; occurrence < counts[index]; occurrence++ {
			price := prices[cursor]
			cursor++
			textTokens := divideTokens(stringCodeUnits(price.Text)) + tokenBlockOverhead
			if price.VisualTokens < 0 || price.VisualTokens > maxInt-textTokens-node.Tokens {
				return nil, 0, fmt.Errorf("harness: route image pricing returned invalid visual tokens for occurrence %d", cursor-1)
			}
			node.Tokens += price.VisualTokens + textTokens
		}
		if node.Tokens < 0 || node.Tokens > maxInt-total {
			return nil, 0, fmt.Errorf("harness: route image pricing exceeded the token measurement range")
		}
		total += node.Tokens
	}
	return nodes, total, nil
}

func tokenContentWithoutImages(blocks []ContentBlock, images *[]ContentBlock) []ContentBlock {
	result := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == BlockImage {
			*images = append(*images, harnessmodel.CloneContentBlock(block))
			continue
		}
		if block.Type == BlockToolResult {
			block.Content = tokenContentWithoutImages(block.Content, images)
		}
		result = append(result, block)
	}
	return result
}

func (meter TokenMeter) imagePricingOptions() ImageRequestPricingOptions {
	return ImageRequestPricingOptions{ResolveReadOnlyPath: func(ref ImageAttachmentRef) (string, error) {
		access, err := harnessmedia.ResolveImageAttachmentAccess(meter.Attachments, ref, nil)
		if err != nil || access == nil {
			return "", err
		}
		return access.ReadonlyPath, nil
	}}
}
