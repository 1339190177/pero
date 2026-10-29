package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	harnessmodel "github.com/phodal/pero/harness/go/model"

	"golang.org/x/image/webp"
)

const (
	DefaultMaxImageBytes               int64 = 20 * 1024 * 1024
	DefaultMaxImagesPerMessage               = 20
	DefaultMaxMessageImageBytes        int64 = 200 * 1024 * 1024
	DefaultMaxImagePixels              int64 = 64_000_000
	DefaultMaxImageDimension                 = 8192
	DefaultNormalizedImageMaxPixels    int64 = 2048 * 2048
	DefaultNormalizedImageMaxDimension       = 8192
	DefaultNormalizedImageMaxBytes     int64 = 4 * 1024 * 1024
	DefaultImageCompressionConcurrency       = 2
	MaxImageCompressionConcurrency           = 8
)

var attachmentIDPattern = regexp.MustCompile(`^sha256:([a-f0-9]{64})$`)

type SaveImageAttachment struct {
	Data      []byte
	MediaType string
	Name      string
}

type EncodedImageAttachment struct {
	Data      string `json:"data"`
	MediaType string `json:"mediaType"`
	Name      string `json:"name,omitempty"`
}

type StoredImageAttachment struct {
	Ref  harnessmodel.ImageAttachmentRef
	Data []byte
}

type AttachmentStore interface {
	SaveImage(context.Context, SaveImageAttachment) (harnessmodel.ImageAttachmentRef, error)
	SaveImages(context.Context, []SaveImageAttachment) ([]harnessmodel.ImageAttachmentRef, error)
	ReadImage(context.Context, harnessmodel.ImageAttachmentRef) (StoredImageAttachment, error)
}

// ImageAttachmentLimits exposes the byte and decoded-size bounds used by a
// durable attachment deployment. Stores may implement AttachmentLimitsProvider
// so read_image can reject oversized source I/O before buffering it.
type ImageAttachmentLimits struct {
	MaxImageBytes        int64    `json:"maxImageBytes"`
	MaxImagesPerMessage  int      `json:"maxImagesPerMessage"`
	MaxMessageImageBytes int64    `json:"maxMessageImageBytes"`
	MaxImagePixels       int64    `json:"maxImagePixels"`
	MaxImageDimension    int      `json:"maxImageDimension"`
	MediaTypes           []string `json:"mediaTypes"`
}

type AttachmentLimitsProvider interface {
	ImageAttachmentLimits() ImageAttachmentLimits
}

// AttachmentValidator lets a custom store expose its exact admission policy
// without publishing an object. Bridges fall back to Point's portable image
// validation when a store does not implement this optional interface.
type AttachmentValidator interface {
	ValidateImage(context.Context, SaveImageAttachment) error
}

type AttachmentError struct {
	Code  string
	Cause error
	Text  string
}

func (e *AttachmentError) Error() string {
	if e == nil {
		return ""
	}
	return e.Text
}

func (e *AttachmentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ToolErrorCode exposes the stable DSH attachment error code to typed Host
// bridges without coupling the attachment package to a transport.
func (e *AttachmentError) ToolErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func newAttachmentError(text, code string, cause error) *AttachmentError {
	return &AttachmentError{Text: text, Code: code, Cause: cause}
}

type LocalAttachmentStoreConfig struct {
	Home                        string
	MaxImageBytes               int64
	MaxImagesPerMessage         int
	MaxMessageImageBytes        int64
	MaxImagePixels              int64
	MaxImageDimension           int
	NormalizedImageMaxPixels    int64
	NormalizedImageMaxDimension int
	NormalizedImageMaxBytes     int64
	ImageCompressionConcurrency int
}

type LocalAttachmentStore struct {
	root            string
	limits          LocalAttachmentStoreConfig
	requestMu       sync.Mutex
	requestInflight map[string]*sharedRequestImage
	compression     chan struct{}
	officialCodec   atomic.Pointer[officialImageCodec]
	codecMu         sync.Mutex
	filePublishMu   sync.Mutex
	codecLeases     int
}

func (s *LocalAttachmentStore) ImageAttachmentLimits() ImageAttachmentLimits {
	if s == nil {
		return ImageAttachmentLimits{}
	}
	return ImageAttachmentLimits{
		MaxImageBytes: s.limits.MaxImageBytes, MaxImagesPerMessage: s.limits.MaxImagesPerMessage,
		MaxMessageImageBytes: s.limits.MaxMessageImageBytes, MaxImagePixels: s.limits.MaxImagePixels,
		MaxImageDimension: s.limits.MaxImageDimension,
		MediaTypes:        []string{"image/png", "image/jpeg", "image/webp", "image/gif"},
	}
}

type preparedImageAttachment struct {
	ref   harnessmodel.ImageAttachmentRef
	data  []byte
	codec *officialImageCodec
}

func OpenLocalAttachmentStore(config LocalAttachmentStoreConfig) (*LocalAttachmentStore, error) {
	if strings.TrimSpace(config.Home) == "" {
		return nil, errors.New("harness: attachment home is required")
	}
	if config.MaxImageBytes == 0 {
		config.MaxImageBytes = DefaultMaxImageBytes
	}
	if config.MaxImagesPerMessage == 0 {
		config.MaxImagesPerMessage = DefaultMaxImagesPerMessage
	}
	if config.MaxMessageImageBytes == 0 {
		config.MaxMessageImageBytes = DefaultMaxMessageImageBytes
	}
	if config.MaxImagePixels == 0 {
		config.MaxImagePixels = DefaultMaxImagePixels
	}
	if config.MaxImageDimension == 0 {
		config.MaxImageDimension = DefaultMaxImageDimension
	}
	if config.NormalizedImageMaxPixels == 0 {
		config.NormalizedImageMaxPixels = DefaultNormalizedImageMaxPixels
	}
	if config.NormalizedImageMaxDimension == 0 {
		config.NormalizedImageMaxDimension = DefaultNormalizedImageMaxDimension
	}
	if config.NormalizedImageMaxBytes == 0 {
		config.NormalizedImageMaxBytes = DefaultNormalizedImageMaxBytes
	}
	if config.ImageCompressionConcurrency == 0 {
		config.ImageCompressionConcurrency = DefaultImageCompressionConcurrency
	}
	if config.MaxImageBytes < 0 || config.MaxImagesPerMessage < 0 || config.MaxMessageImageBytes < 0 ||
		config.MaxImagePixels < 0 || config.MaxImageDimension < 0 || config.NormalizedImageMaxPixels < 0 ||
		config.NormalizedImageMaxDimension < 0 || config.NormalizedImageMaxBytes < 0 ||
		config.ImageCompressionConcurrency < 1 || config.ImageCompressionConcurrency > MaxImageCompressionConcurrency {
		return nil, errors.New("harness: attachment limits must be positive")
	}
	home, err := filepath.Abs(config.Home)
	if err != nil {
		return nil, fmt.Errorf("harness: resolve attachment home: %w", err)
	}
	return &LocalAttachmentStore{
		root: filepath.Join(home, "attachments", "v1"), limits: config,
		compression: make(chan struct{}, config.ImageCompressionConcurrency),
	}, nil
}

func (s *LocalAttachmentStore) Root() string { return s.root }

// Config returns the limits and absolute home used by trusted Host adapters
// that share this content-addressed store with another runtime.
func (s *LocalAttachmentStore) Config() LocalAttachmentStoreConfig {
	config := s.limits
	config.Home = filepath.Dir(filepath.Dir(s.root))
	return config
}

func (s *LocalAttachmentStore) SaveImage(ctx context.Context, input SaveImageAttachment) (harnessmodel.ImageAttachmentRef, error) {
	refs, err := s.SaveImages(ctx, []SaveImageAttachment{input})
	if err != nil {
		return harnessmodel.ImageAttachmentRef{}, err
	}
	return refs[0], nil
}

// ValidateImage runs complete normalization admission without publishing bytes.
func (s *LocalAttachmentStore) ValidateImage(ctx context.Context, input SaveImageAttachment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.MediaType != "image/png" && input.MediaType != "image/jpeg" && input.MediaType != "image/webp" && input.MediaType != "image/gif" {
		return newAttachmentError("Image type is not accepted by this deployment.", "UNSUPPORTED_IMAGE_TYPE", nil)
	}
	_, err := s.prepare(ctx, input)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *LocalAttachmentStore) SaveImages(ctx context.Context, inputs []SaveImageAttachment) ([]harnessmodel.ImageAttachmentRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) > s.limits.MaxImagesPerMessage {
		return nil, newAttachmentError("Image batch exceeds the configured count limit.", "TOO_MANY_IMAGES", nil)
	}
	var total int64
	prepared := make([]preparedImageAttachment, len(inputs))
	for index, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if input.MediaType != "image/png" && input.MediaType != "image/jpeg" && input.MediaType != "image/webp" && input.MediaType != "image/gif" {
			return nil, newAttachmentError("Image type is not accepted by this deployment.", "UNSUPPORTED_IMAGE_TYPE", nil)
		}
		if int64(len(input.Data)) > s.limits.MaxMessageImageBytes-total {
			return nil, newAttachmentError("Image batch exceeds the configured byte limit.", "IMAGES_TOO_LARGE", nil)
		}
		total += int64(len(input.Data))
		image, err := s.prepare(ctx, input)
		if err != nil {
			return nil, err
		}
		prepared[index] = image
	}
	refs := make([]harnessmodel.ImageAttachmentRef, len(prepared))
	for index, image := range prepared {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.commit(image); err != nil {
			return nil, err
		}
		refs[index] = image.ref
	}
	return refs, nil
}

func (s *LocalAttachmentStore) prepare(ctx context.Context, input SaveImageAttachment) (preparedImageAttachment, error) {
	if len(input.Data) == 0 {
		return preparedImageAttachment{}, newAttachmentError("Image is empty.", "INVALID_IMAGE", nil)
	}
	if int64(len(input.Data)) > s.limits.MaxImageBytes {
		return preparedImageAttachment{}, newAttachmentError("Image exceeds the configured byte limit.", "IMAGE_TOO_LARGE", nil)
	}
	if codec := s.officialCodec.Load(); codec != nil {
		if err := s.acquireCompression(ctx); err != nil {
			return preparedImageAttachment{}, err
		}
		defer s.releaseCompression()
		return codec.prepare(ctx, input, s.limits)
	}
	width, height, detected, err := probeImageMetadata(input.Data)
	if err != nil {
		return preparedImageAttachment{}, err
	}
	if detected != input.MediaType {
		return preparedImageAttachment{}, newAttachmentError("Declared image type does not match its bytes.", "IMAGE_TYPE_MISMATCH", nil)
	}
	orientation, err := imageOrientation(input.Data, detected)
	if err != nil {
		return preparedImageAttachment{}, err
	}
	if orientation >= 5 {
		width, height = height, width
	}
	if width <= 0 || height <= 0 || int64(width) > s.limits.MaxImagePixels/int64(height) {
		return preparedImageAttachment{}, newAttachmentError("Image exceeds the configured decoded-pixel limit.", "IMAGE_TOO_MANY_PIXELS", nil)
	}
	if width > s.limits.MaxImageDimension || height > s.limits.MaxImageDimension {
		return preparedImageAttachment{}, newAttachmentError("Image exceeds the configured per-side pixel limit.", "IMAGE_DIMENSION_TOO_LARGE", nil)
	}
	if err := s.acquireCompression(ctx); err != nil {
		return preparedImageAttachment{}, err
	}
	defer s.releaseCompression()
	normalized, err := normalizeImage(input.Data, detected, orientation, imageNormalizationPolicy{
		MaxPixels: s.limits.NormalizedImageMaxPixels, MaxDimension: s.limits.NormalizedImageMaxDimension,
		MaxBytes: s.limits.NormalizedImageMaxBytes,
	})
	if err != nil {
		return preparedImageAttachment{}, err
	}
	digest := sha256.Sum256(normalized.Data)
	ref := harnessmodel.ImageAttachmentRef{
		AttachmentID: "sha256:" + hex.EncodeToString(digest[:]), MediaType: normalized.MediaType,
		Bytes: int64(len(normalized.Data)), Width: normalized.Width, Height: normalized.Height, Name: attachmentDisplayName(input.Name),
	}
	if normalized.Width != width || normalized.Height != height {
		ref.OriginalDimensions = &harnessmodel.ImageDimensions{Width: width, Height: height}
	}
	return preparedImageAttachment{ref: ref, data: normalized.Data}, nil
}

func (s *LocalAttachmentStore) acquireCompression(ctx context.Context) error {
	select {
	case s.compression <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *LocalAttachmentStore) releaseCompression() { <-s.compression }

func (s *LocalAttachmentStore) commit(image preparedImageAttachment) error {
	s.codecMu.Lock()
	defer s.codecMu.Unlock()
	if image.codec != s.officialCodec.Load() {
		return newAttachmentError("Image codec changed before attachment publication.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	if image.codec != nil {
		if err := image.codec.activeErrorLocked(); err != nil {
			return err
		}
	}
	hash, err := attachmentHash(image.ref)
	if err != nil {
		return err
	}
	bucket := filepath.Join(s.root, "objects", hash[:2])
	staging := filepath.Join(s.root, "tmp")
	for _, directory := range []string{s.root, filepath.Join(s.root, "objects"), bucket, staging} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
		}
	}
	temporary, err := os.CreateTemp(staging, "pending-*")
	if err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err = temporary.Chmod(0o600); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if _, err = temporary.Write(image.data); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err = temporary.Sync(); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err = temporary.Close(); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	target := filepath.Join(bucket, hash)
	if err = os.Link(temporaryName, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
		}
		existing, readErr := os.ReadFile(target)
		if readErr != nil || sha256.Sum256(existing) != sha256.Sum256(image.data) {
			return newAttachmentError("Stored attachment failed integrity verification.", "ATTACHMENT_CORRUPT", readErr)
		}
	}
	// Remove the staging link before setting Windows' shared read-only attribute.
	if err = os.Remove(temporaryName); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err = os.Chmod(target, 0o400); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err = syncDirectory(bucket); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err = syncDirectory(filepath.Join(s.root, "objects")); err != nil {
		return newAttachmentError("Unable to persist image attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	return nil
}

// ImageHostPath is the immutable normalized object path. It does not read the
// file; an invalid attachment id fails before a path is returned.
func (s *LocalAttachmentStore) ImageHostPath(ref harnessmodel.ImageAttachmentRef) (string, error) {
	if s == nil {
		return "", errors.New("harness: attachment store is required")
	}
	hash, err := attachmentHash(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, "objects", hash[:2], hash), nil
}

func (s *LocalAttachmentStore) ReadImage(ctx context.Context, ref harnessmodel.ImageAttachmentRef) (StoredImageAttachment, error) {
	if err := ctx.Err(); err != nil {
		return StoredImageAttachment{}, err
	}
	hash, err := attachmentHash(ref)
	if err != nil {
		return StoredImageAttachment{}, err
	}
	data, err := os.ReadFile(filepath.Join(s.root, "objects", hash[:2], hash))
	if errors.Is(err, os.ErrNotExist) {
		return StoredImageAttachment{}, newAttachmentError("Attachment object is missing.", "ATTACHMENT_NOT_FOUND", err)
	}
	if err != nil {
		return StoredImageAttachment{}, newAttachmentError("Unable to read image attachment.", "ATTACHMENT_READ_FAILED", err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != hash {
		return StoredImageAttachment{}, newAttachmentError("Stored attachment failed integrity verification.", "ATTACHMENT_CORRUPT", nil)
	}
	width, height, mediaType, metadataErr := probeImageMetadata(data)
	if metadataErr != nil || mediaType != ref.MediaType || int64(len(data)) != ref.Bytes || width != ref.Width || height != ref.Height {
		return StoredImageAttachment{}, newAttachmentError("Stored attachment metadata does not match its reference.", "ATTACHMENT_CORRUPT", metadataErr)
	}
	return StoredImageAttachment{Ref: ref, Data: data}, nil
}

// ValidateImageAttachment performs the provider-independent DSH image
// admission checks without persisting the image. Custom stores can use it to
// implement AttachmentValidator; the DSH bridge also uses it as a safe
// fallback for stores that only implement AttachmentStore.
func ValidateImageAttachment(ctx context.Context, input SaveImageAttachment, limits ImageAttachmentLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	limits = resolvedImageAttachmentLimits(limits)
	if len(input.Data) == 0 {
		return newAttachmentError("Image is empty.", "INVALID_IMAGE", nil)
	}
	if int64(len(input.Data)) > limits.MaxImageBytes {
		return newAttachmentError("Image exceeds the configured byte limit.", "IMAGE_TOO_LARGE", nil)
	}
	accepted := false
	for _, mediaType := range limits.MediaTypes {
		if input.MediaType == mediaType {
			accepted = true
			break
		}
	}
	if !accepted {
		return newAttachmentError("Image type is not accepted by this deployment.", "UNSUPPORTED_IMAGE_TYPE", nil)
	}
	width, height, detected, err := probeImageMetadata(input.Data)
	if err != nil {
		return err
	}
	if detected != input.MediaType {
		return newAttachmentError("Declared image type does not match its bytes.", "IMAGE_TYPE_MISMATCH", nil)
	}
	orientation, err := imageOrientation(input.Data, detected)
	if err != nil {
		return err
	}
	if orientation >= 5 {
		width, height = height, width
	}
	if width <= 0 || height <= 0 || int64(width) > limits.MaxImagePixels/int64(height) {
		return newAttachmentError("Image exceeds the configured decoded-pixel limit.", "IMAGE_TOO_MANY_PIXELS", nil)
	}
	if width > limits.MaxImageDimension || height > limits.MaxImageDimension {
		return newAttachmentError("Image exceeds the configured per-side pixel limit.", "IMAGE_DIMENSION_TOO_LARGE", nil)
	}
	if _, err = decodeStoredImage(input.Data, detected); err != nil {
		return newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
	}
	return ctx.Err()
}

// VerifyStoredImageAttachment checks the immutable reference, digest, byte
// count, media type, and dimensions before bytes cross a Host bridge.
func VerifyStoredImageAttachment(stored StoredImageAttachment) error {
	hash, err := attachmentHash(stored.Ref)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(stored.Data)
	if hex.EncodeToString(digest[:]) != hash {
		return newAttachmentError("Stored attachment failed integrity verification.", "ATTACHMENT_CORRUPT", nil)
	}
	width, height, mediaType, metadataErr := probeImageMetadata(stored.Data)
	if metadataErr != nil || mediaType != stored.Ref.MediaType || int64(len(stored.Data)) != stored.Ref.Bytes ||
		width != stored.Ref.Width || height != stored.Ref.Height {
		return newAttachmentError("Stored attachment metadata does not match its reference.", "ATTACHMENT_CORRUPT", metadataErr)
	}
	return nil
}

func resolvedImageAttachmentLimits(limits ImageAttachmentLimits) ImageAttachmentLimits {
	if limits.MaxImageBytes <= 0 {
		limits.MaxImageBytes = DefaultMaxImageBytes
	}
	if limits.MaxImagesPerMessage <= 0 {
		limits.MaxImagesPerMessage = DefaultMaxImagesPerMessage
	}
	if limits.MaxMessageImageBytes <= 0 {
		limits.MaxMessageImageBytes = DefaultMaxMessageImageBytes
	}
	if limits.MaxImagePixels <= 0 {
		limits.MaxImagePixels = DefaultMaxImagePixels
	}
	if limits.MaxImageDimension <= 0 {
		limits.MaxImageDimension = DefaultMaxImageDimension
	}
	if len(limits.MediaTypes) == 0 {
		limits.MediaTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	} else {
		limits.MediaTypes = append([]string(nil), limits.MediaTypes...)
	}
	return limits
}

func AdmitEncodedImages(ctx context.Context, store AttachmentStore, images []EncodedImageAttachment) ([]harnessmodel.ImageAttachmentRef, error) {
	if store == nil {
		return nil, errors.New("harness: attachment store is required")
	}
	inputs := make([]SaveImageAttachment, len(images))
	for index, image := range images {
		if image.Data == "" {
			return nil, newAttachmentError("Image upload is not canonical base64.", "INVALID_IMAGE_BASE64", nil)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(image.Data)
		if err != nil || base64.StdEncoding.EncodeToString(decoded) != image.Data {
			return nil, newAttachmentError("Image upload is not canonical base64.", "INVALID_IMAGE_BASE64", err)
		}
		inputs[index] = SaveImageAttachment{Data: decoded, MediaType: image.MediaType, Name: image.Name}
	}
	return store.SaveImages(ctx, inputs)
}

// MaterializeContentImages persists inline images and returns a detached
// content tree containing durable attachment references.
func MaterializeContentImages(ctx context.Context, store AttachmentStore, blocks []harnessmodel.ContentBlock) ([]harnessmodel.ContentBlock, error) {
	result := harnessmodel.CloneContentBlocks(blocks)
	if store == nil {
		return result, nil
	}
	var inputs []SaveImageAttachment
	var targets []*harnessmodel.ContentBlock
	var collect func([]harnessmodel.ContentBlock) error
	collect = func(content []harnessmodel.ContentBlock) error {
		for index := range content {
			block := &content[index]
			if block.Type == harnessmodel.BlockImage {
				if block.Attachment != nil && (len(block.Data) != 0 || block.MIMEType != "") {
					return newAttachmentError("Image block mixes durable reference and inline bytes.", "INVALID_ATTACHMENT_REF", nil)
				}
				if block.Attachment == nil {
					inputs = append(inputs, SaveImageAttachment{Data: block.Data, MediaType: block.MIMEType, Name: block.Name})
					targets = append(targets, block)
				}
			}
			if err := collect(block.Content); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(result); err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return result, nil
	}
	refs, err := store.SaveImages(ctx, inputs)
	if err != nil {
		return nil, err
	}
	for index, ref := range refs {
		targets[index].Data = nil
		targets[index].MIMEType = ""
		targets[index].Name = ""
		attachment := ref
		targets[index].Attachment = &attachment
	}
	return result, nil
}

func attachmentHash(ref harnessmodel.ImageAttachmentRef) (string, error) {
	match := attachmentIDPattern.FindStringSubmatch(ref.AttachmentID)
	if len(match) != 2 {
		return "", newAttachmentError("Attachment reference is invalid.", "INVALID_ATTACHMENT_REF", nil)
	}
	return match[1], nil
}

func attachmentDisplayName(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "/")
	value = filepath.Base(value)
	value = strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return -1
		}
		return char
	}, value)
	value = strings.TrimSpace(value)
	characters := []rune(value)
	if len(characters) > 255 {
		value = string(characters[:255])
	}
	return value
}

func probeImageMetadata(data []byte) (int, int, string, error) {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		decoded, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, "", newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
		}
		return decoded.Width, decoded.Height, "image/png", nil
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		decoded, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, "", newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
		}
		return decoded.Width, decoded.Height, "image/jpeg", nil
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		decoded, err := gif.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, "", newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
		}
		return decoded.Width, decoded.Height, "image/gif", nil
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		decoded, err := webp.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, "", newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
		}
		return decoded.Width, decoded.Height, "image/webp", nil
	default:
		return 0, 0, "", newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", nil)
	}
}

func syncDirectory(path string) error {
	// Windows cannot flush a directory opened with os.Open. Object files are
	// synced before publication; as in session persistence, we do not claim
	// the additional POSIX directory-entry power-loss guarantee on Windows.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
