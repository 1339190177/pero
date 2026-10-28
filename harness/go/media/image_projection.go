package media

import (
	"errors"
	"fmt"
	"math"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

type ImageRequestRepresentation string

const (
	ImageRepresentationRaw    ImageRequestRepresentation = "raw"
	ImageRepresentationBase64 ImageRequestRepresentation = "base64"
)

// ImageRequestOffloadPolicy describes one provider representation's request
// budgets. Zero max values are unbounded; zero quanta use a one-unit step.
type ImageRequestOffloadPolicy struct {
	MaxImages      int
	MaxBytes       int64
	CountQuantum   int
	ByteQuantum    int64
	Representation ImageRequestRepresentation
	ByteLength     func(harnessmodel.ContentBlock) (int64, error)
	Placeholder    func(harnessmodel.ContentBlock) (string, error)
}

// OffloadedImagePrefixCount returns the number of oldest occurrences removed
// in whole count and byte quanta. The strict byte crossing for multi-byte
// quanta matches DSH's stable high-watermark/low-watermark behavior.
func OffloadedImagePrefixCount(lengths []int64, maxImages int, maxBytes int64, countQuantum int, byteQuantum int64) (int, error) {
	if maxImages < 0 || maxBytes < 0 || countQuantum < 0 || byteQuantum < 0 {
		return 0, errors.New("harness: image offload limits must not be negative")
	}
	if countQuantum == 0 {
		countQuantum = 1
	}
	if byteQuantum == 0 {
		byteQuantum = 1
	}
	var total int64
	for _, length := range lengths {
		if length < 0 || length > math.MaxInt64-total {
			return 0, errors.New("harness: invalid represented image length")
		}
		total += length
	}
	excessCount := 0
	if maxImages > 0 && len(lengths) > maxImages {
		excessCount = len(lengths) - maxImages
	}
	excessBytes := int64(0)
	if maxBytes > 0 && total > maxBytes {
		excessBytes = total - maxBytes
	}
	if excessCount == 0 && excessBytes == 0 {
		return 0, nil
	}
	removeCount := 0
	if excessCount != 0 {
		quanta := (excessCount-1)/countQuantum + 1
		if quanta > math.MaxInt/countQuantum {
			removeCount = math.MaxInt
		} else {
			removeCount = quanta * countQuantum
		}
	}
	removeBytes := int64(0)
	if excessBytes != 0 {
		quanta := (excessBytes-1)/byteQuantum + 1
		if quanta > math.MaxInt64/byteQuantum {
			removeBytes = math.MaxInt64
		} else {
			removeBytes = quanta * byteQuantum
		}
	}
	count := 0
	removedBytes := int64(0)
	for _, length := range lengths {
		byteTargetMet := removeBytes == 0 || removedBytes >= removeBytes
		if byteQuantum != 1 && removeBytes != 0 {
			byteTargetMet = removedBytes > removeBytes
		}
		if count >= removeCount && byteTargetMet {
			break
		}
		removedBytes += length
		count++
	}
	return count, nil
}

// ImageOffloadRequiredError is the count of oldest retained occurrences a
// route still needs omitted. The route reports it instead of dropping images
// itself; the session records the durable selection and retries.
type ImageOffloadRequiredError struct {
	Count int
}

func (e *ImageOffloadRequiredError) Error() string {
	count := 0
	if e != nil {
		count = e.Count
	}
	return fmt.Sprintf("request images exceed the route budget; %d more oldest occurrence(s) must be offloaded", count)
}

// ProjectOffloadedImages replaces occurrences already marked offloaded with
// placeholder text. Unmarked images stay images.
func ProjectOffloadedImages(messages []harnessmodel.Message, placeholder func(harnessmodel.ContentBlock) (string, error)) ([]harnessmodel.Message, error) {
	if placeholder == nil {
		return nil, errors.New("harness: image offload placeholder is required")
	}
	result := append([]harnessmodel.Message(nil), messages...)
	changedAny := false
	for index, message := range result {
		content, changed, err := replaceOffloadedImageBlocks(message.Content, placeholder)
		if err != nil {
			return nil, err
		}
		if changed {
			result[index].Content = content
			changedAny = true
		}
	}
	if !changedAny {
		return messages, nil
	}
	return result, nil
}

// RequiredImageOffload returns how many more oldest retained occurrences the
// policy removes. Occurrences already marked offloaded are not counted.
func RequiredImageOffload(messages []harnessmodel.Message, policy ImageRequestOffloadPolicy) (int, error) {
	if policy.Representation != ImageRepresentationRaw && policy.Representation != ImageRepresentationBase64 {
		return 0, errors.New("harness: invalid image request representation")
	}
	lengths := make([]int64, 0)
	var walk func([]harnessmodel.ContentBlock) error
	walk = func(blocks []harnessmodel.ContentBlock) error {
		for _, block := range blocks {
			if block.Type == harnessmodel.BlockImage {
				if block.Offloaded {
					continue
				}
				length, err := representedImageLength(block, policy)
				if err != nil {
					return err
				}
				lengths = append(lengths, length)
				continue
			}
			if block.Type == harnessmodel.BlockToolResult {
				if err := walk(block.Content); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, message := range messages {
		if err := walk(message.Content); err != nil {
			return 0, err
		}
	}
	return OffloadedImagePrefixCount(lengths, policy.MaxImages, policy.MaxBytes, policy.CountQuantum, policy.ByteQuantum)
}

// OffloadRequestImages returns a transient projection that replaces the
// deterministic oldest image prefix without mutating durable messages.
// Image-capable routes report RequiredImageOffload and leave this projection
// for callers that still want the replaced messages directly.
func OffloadRequestImages(messages []harnessmodel.Message, policy ImageRequestOffloadPolicy) ([]harnessmodel.Message, error) {
	if policy.Representation != ImageRepresentationRaw && policy.Representation != ImageRepresentationBase64 {
		return nil, errors.New("harness: invalid image request representation")
	}
	if policy.Placeholder == nil {
		return nil, errors.New("harness: image offload placeholder is required")
	}
	var images []harnessmodel.ContentBlock
	for _, message := range messages {
		collectImageBlocks(message.Content, &images)
	}
	lengths := make([]int64, len(images))
	for index, block := range images {
		length, err := representedImageLength(block, policy)
		if err != nil {
			return nil, err
		}
		lengths[index] = length
	}
	count, err := OffloadedImagePrefixCount(lengths, policy.MaxImages, policy.MaxBytes, policy.CountQuantum, policy.ByteQuantum)
	if err != nil || count == 0 {
		return messages, err
	}
	result := append([]harnessmodel.Message(nil), messages...)
	remaining := count
	for index, message := range result {
		content, replaced, replaceErr := replaceOldestImageBlocks(message.Content, &remaining, policy.Placeholder)
		if replaceErr != nil {
			return nil, replaceErr
		}
		if replaced {
			result[index].Content = content
		}
	}
	return result, nil
}

// ProjectImagesForTextModel replaces every image occurrence with the stable
// text-only placeholder used by DSH for a route without image modality.
func ProjectImagesForTextModel(messages []harnessmodel.Message) []harnessmodel.Message {
	result := append([]harnessmodel.Message(nil), messages...)
	remaining := math.MaxInt
	for index, message := range result {
		content, replaced, _ := replaceOldestImageBlocks(message.Content, &remaining, func(block harnessmodel.ContentBlock) (string, error) {
			return TextOnlyImageText(block), nil
		})
		if replaced {
			result[index].Content = content
		}
	}
	return result
}

func TextOnlyImageText(block harnessmodel.ContentBlock) string {
	if block.Attachment == nil {
		return "[image omitted because this model accepts text only; no durable attachment identity is available]"
	}
	digest := block.Attachment.AttachmentID
	if len(digest) > len("sha256:")+8 {
		digest = digest[:len("sha256:")+8]
	}
	return "[image omitted because this model accepts text only; attachment " + digest + "]"
}

func OffloadedImageText(block harnessmodel.ContentBlock) string {
	return OffloadedImageTextWithAccess(block, nil)
}

// OffloadedImageTextWithAccess adds the execution-world read-only path when the
// attachment store and path mapper both resolve one.
func OffloadedImageTextWithAccess(block harnessmodel.ContentBlock, access *ImageAttachmentAccess) string {
	path := ""
	if access != nil {
		path = access.ReadonlyPath
	}
	return harnessmodel.OffloadedImageText(block, path)
}

// OffloadedImagePlaceholder resolves a local read-only path, then builds the
// same placeholder the request encoder sends. A store without a host path, or
// a mapper that declines the path, keeps the no-path sentence. An invalid
// durable reference is returned to the caller.
func OffloadedImagePlaceholder(store AttachmentStore, block harnessmodel.ContentBlock) (string, error) {
	if block.Attachment == nil || store == nil {
		return OffloadedImageText(block), nil
	}
	access, err := ResolveImageAttachmentAccess(store, *block.Attachment, nil)
	if err != nil {
		return "", err
	}
	return OffloadedImageTextWithAccess(block, access), nil
}

func RequestImageHandleText(ref harnessmodel.ImageAttachmentRef, width, height int) string {
	return RequestImageHandleTextWithAccess(ref, width, height, nil)
}

func RequestImageHandleTextWithAccess(ref harnessmodel.ImageAttachmentRef, width, height int, access *ImageAttachmentAccess) string {
	path := ""
	if access != nil {
		path = access.ReadonlyPath
	}
	return harnessmodel.RequestImageHandleText(ref, width, height, path)
}

func imageAttachmentIdentity(ref harnessmodel.ImageAttachmentRef) string {
	return harnessmodel.ImageAttachmentIdentity(ref)
}

func collectImageBlocks(blocks []harnessmodel.ContentBlock, images *[]harnessmodel.ContentBlock) {
	for _, block := range blocks {
		if block.Type == harnessmodel.BlockImage {
			*images = append(*images, block)
		} else if block.Type == harnessmodel.BlockToolResult {
			collectImageBlocks(block.Content, images)
		}
	}
}

func representedImageLength(block harnessmodel.ContentBlock, policy ImageRequestOffloadPolicy) (int64, error) {
	var length int64
	var err error
	if policy.ByteLength != nil {
		length, err = policy.ByteLength(block)
	} else if block.Attachment != nil {
		length = block.Attachment.Bytes
	} else {
		length = int64(len(block.Data))
	}
	if err != nil || length < 0 {
		return 0, errors.Join(errors.New("harness: resolve represented image length"), err)
	}
	if policy.Representation == ImageRepresentationBase64 {
		groups := length / 3
		if length%3 != 0 {
			groups++
		}
		if groups > math.MaxInt64/4 {
			return 0, errors.New("harness: image is too large to represent")
		}
		length = groups * 4
	}
	return length, nil
}

func replaceOffloadedImageBlocks(
	blocks []harnessmodel.ContentBlock,
	placeholder func(harnessmodel.ContentBlock) (string, error),
) ([]harnessmodel.ContentBlock, bool, error) {
	var result []harnessmodel.ContentBlock
	changed := false
	for index, block := range blocks {
		if block.Type == harnessmodel.BlockImage && block.Offloaded {
			text, err := placeholder(block)
			if err != nil {
				return nil, false, err
			}
			if !changed {
				result = append(make([]harnessmodel.ContentBlock, 0, len(blocks)), blocks[:index]...)
				changed = true
			}
			result = append(result, harnessmodel.ContentBlock{Type: harnessmodel.BlockText, Text: text})
			continue
		}
		if block.Type == harnessmodel.BlockToolResult {
			content, replaced, err := replaceOffloadedImageBlocks(block.Content, placeholder)
			if err != nil {
				return nil, false, err
			}
			if replaced {
				if !changed {
					result = append(make([]harnessmodel.ContentBlock, 0, len(blocks)), blocks[:index]...)
					changed = true
				}
				block.Content = content
			}
		}
		if changed {
			result = append(result, block)
		}
	}
	if !changed {
		return blocks, false, nil
	}
	return result, true, nil
}

func replaceOldestImageBlocks(
	blocks []harnessmodel.ContentBlock,
	remaining *int,
	placeholder func(harnessmodel.ContentBlock) (string, error),
) ([]harnessmodel.ContentBlock, bool, error) {
	var result []harnessmodel.ContentBlock
	changed := false
	for index, block := range blocks {
		if block.Type == harnessmodel.BlockImage && *remaining > 0 {
			text, err := placeholder(block)
			if err != nil {
				return nil, false, err
			}
			*remaining--
			if !changed {
				result = append(make([]harnessmodel.ContentBlock, 0, len(blocks)), blocks[:index]...)
				changed = true
			}
			result = append(result, harnessmodel.ContentBlock{Type: harnessmodel.BlockText, Text: text})
			continue
		}
		if block.Type == harnessmodel.BlockToolResult {
			content, replaced, err := replaceOldestImageBlocks(block.Content, remaining, placeholder)
			if err != nil {
				return nil, false, err
			}
			if replaced {
				if !changed {
					result = append(make([]harnessmodel.ContentBlock, 0, len(blocks)), blocks[:index]...)
					changed = true
				}
				block.Content = content
			}
		}
		if changed {
			result = append(result, block)
		}
	}
	if !changed {
		return blocks, false, nil
	}
	return result, true, nil
}
