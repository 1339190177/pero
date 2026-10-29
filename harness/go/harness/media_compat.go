package harness

import (
	"context"

	harnessmedia "github.com/phodal/pero/harness/go/media"
)

const (
	DefaultMaxImageBytes               = harnessmedia.DefaultMaxImageBytes
	DefaultMaxImagesPerMessage         = harnessmedia.DefaultMaxImagesPerMessage
	DefaultMaxMessageImageBytes        = harnessmedia.DefaultMaxMessageImageBytes
	DefaultMaxImagePixels              = harnessmedia.DefaultMaxImagePixels
	DefaultMaxImageDimension           = harnessmedia.DefaultMaxImageDimension
	DefaultNormalizedImageMaxPixels    = harnessmedia.DefaultNormalizedImageMaxPixels
	DefaultNormalizedImageMaxDimension = harnessmedia.DefaultNormalizedImageMaxDimension
	DefaultNormalizedImageMaxBytes     = harnessmedia.DefaultNormalizedImageMaxBytes
	DefaultImageCompressionConcurrency = harnessmedia.DefaultImageCompressionConcurrency
	MaxImageCompressionConcurrency     = harnessmedia.MaxImageCompressionConcurrency
	RequestImageTransformVersion       = harnessmedia.RequestImageTransformVersion
)

type SaveFileAttachment = harnessmedia.SaveFileAttachment
type SaveFileStreamAttachment = harnessmedia.SaveFileStreamAttachment
type EncodedFileAttachment = harnessmedia.EncodedFileAttachment
type FileAttachmentStore = harnessmedia.FileAttachmentStore
type SaveImageAttachment = harnessmedia.SaveImageAttachment
type EncodedImageAttachment = harnessmedia.EncodedImageAttachment
type StoredImageAttachment = harnessmedia.StoredImageAttachment
type AttachmentStore = harnessmedia.AttachmentStore
type ImageAttachmentLimits = harnessmedia.ImageAttachmentLimits
type AttachmentLimitsProvider = harnessmedia.AttachmentLimitsProvider
type AttachmentValidator = harnessmedia.AttachmentValidator
type AttachmentError = harnessmedia.AttachmentError
type LocalAttachmentStoreConfig = harnessmedia.LocalAttachmentStoreConfig
type LocalAttachmentStore = harnessmedia.LocalAttachmentStore
type ImageRequestPolicy = harnessmedia.ImageRequestPolicy
type RequestImageAttachment = harnessmedia.RequestImageAttachment
type RequestImageStore = harnessmedia.RequestImageStore

var requestImageJPEGQualities = harnessmedia.RequestImageJPEGQualities()

func OpenLocalAttachmentStore(config LocalAttachmentStoreConfig) (*LocalAttachmentStore, error) {
	return harnessmedia.OpenLocalAttachmentStore(config)
}

func ValidateImageAttachment(ctx context.Context, input SaveImageAttachment, limits ImageAttachmentLimits) error {
	return harnessmedia.ValidateImageAttachment(ctx, input, limits)
}

func VerifyStoredImageAttachment(stored StoredImageAttachment) error {
	return harnessmedia.VerifyStoredImageAttachment(stored)
}

func AdmitEncodedImages(ctx context.Context, store AttachmentStore, images []EncodedImageAttachment) ([]ImageAttachmentRef, error) {
	return harnessmedia.AdmitEncodedImages(ctx, store, images)
}

func materializeContentImages(ctx context.Context, store AttachmentStore, blocks []ContentBlock) ([]ContentBlock, error) {
	return harnessmedia.MaterializeContentImages(ctx, store, blocks)
}

func RequestImageDimensions(width, height int, maxPixels int64) (int, int, error) {
	return harnessmedia.RequestImageDimensions(width, height, maxPixels)
}

func RequestImageVariantID(ref ImageAttachmentRef, policy ImageRequestPolicy) (string, error) {
	return harnessmedia.RequestImageVariantID(ref, policy)
}

// These aliases preserve the root-package API while media owns transient
// request-image projection. New domain packages should import media directly.
type ImageRequestRepresentation = harnessmedia.ImageRequestRepresentation

const (
	ImageRepresentationRaw    = harnessmedia.ImageRepresentationRaw
	ImageRepresentationBase64 = harnessmedia.ImageRepresentationBase64
)

type ImageRequestOffloadPolicy = harnessmedia.ImageRequestOffloadPolicy

func OffloadedImagePrefixCount(lengths []int64, maxImages int, maxBytes int64, countQuantum int, byteQuantum int64) (int, error) {
	return harnessmedia.OffloadedImagePrefixCount(lengths, maxImages, maxBytes, countQuantum, byteQuantum)
}

func OffloadRequestImages(messages []Message, policy ImageRequestOffloadPolicy) ([]Message, error) {
	return harnessmedia.OffloadRequestImages(messages, policy)
}

func ProjectImagesForTextModel(messages []Message) []Message {
	return harnessmedia.ProjectImagesForTextModel(messages)
}

func TextOnlyImageText(block ContentBlock) string {
	return harnessmedia.TextOnlyImageText(block)
}

func OffloadedImageText(block ContentBlock) string {
	return harnessmedia.OffloadedImageText(block)
}

func RequestImageHandleText(ref ImageAttachmentRef, width, height int) string {
	return harnessmedia.RequestImageHandleText(ref, width, height)
}

func AdmitEncodedFile(ctx context.Context, store FileAttachmentStore, input EncodedFileAttachment) (FileAttachmentRef, error) {
	return harnessmedia.AdmitEncodedFile(ctx, store, input)
}
