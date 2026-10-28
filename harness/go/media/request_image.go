package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"

	harnessmodel "github.com/phodal/pero/harness/go/model"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const RequestImageTransformVersion = "go-request-image-v2"

var requestImageJPEGQualities = [...]int{85, 75, 60}

// RequestImageJPEGQualities returns the deterministic opaque-image encoding
// sequence used by the request-image transform.
func RequestImageJPEGQualities() []int {
	return append([]int(nil), requestImageJPEGQualities[:]...)
}

type ImageRequestPolicy struct {
	MaxPixels int64 `json:"maxPixels"`
	MaxBytes  int64 `json:"maxBytes"`
	// Width and Height select an exact route-owned preview when both are set.
	// MaxPixels remains the legacy total-pixel budget for other callers.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type RequestImageAttachment struct {
	VariantID  string                          `json:"variantId"`
	Attachment harnessmodel.ImageAttachmentRef `json:"attachment"`
	Data       []byte                          `json:"-"`
	MediaType  string                          `json:"mediaType"`
	Bytes      int64                           `json:"bytes"`
	Width      int                             `json:"width"`
	Height     int                             `json:"height"`
	Depth      string                          `json:"depth"`
	Space      string                          `json:"space"`
	HasAlpha   bool                            `json:"hasAlpha"`
}

type RequestImageStore interface {
	ReadRequestImage(context.Context, harnessmodel.ImageAttachmentRef, ImageRequestPolicy) (RequestImageAttachment, error)
}

type sharedRequestImage struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	settled bool
	result  RequestImageAttachment
	err     error
}

func RequestImageDimensions(width, height int, maxPixels int64) (int, int, error) {
	if width <= 0 || height <= 0 || maxPixels <= 0 {
		return 0, 0, newAttachmentError("Image request dimensions require positive integers.", "INVALID_ATTACHMENT_REF", nil)
	}
	pixels := float64(width) * float64(height)
	scale := math.Min(1, math.Sqrt(float64(maxPixels)/pixels))
	if scale == 1 {
		return width, height, nil
	}
	if width >= height {
		projectedWidth := max(1, int(math.Floor(float64(width)*scale)))
		projectedHeight := max(1, int(math.Round(float64(projectedWidth)*float64(height)/float64(width))))
		for int64(projectedWidth)*int64(projectedHeight) > maxPixels && projectedWidth > 1 {
			projectedWidth--
			projectedHeight = max(1, int(math.Round(float64(projectedWidth)*float64(height)/float64(width))))
		}
		return projectedWidth, projectedHeight, nil
	}
	projectedHeight := max(1, int(math.Floor(float64(height)*scale)))
	projectedWidth := max(1, int(math.Round(float64(projectedHeight)*float64(width)/float64(height))))
	for int64(projectedWidth)*int64(projectedHeight) > maxPixels && projectedHeight > 1 {
		projectedHeight--
		projectedWidth = max(1, int(math.Round(float64(projectedHeight)*float64(width)/float64(height))))
	}
	return projectedWidth, projectedHeight, nil
}

func RequestImageVariantID(ref harnessmodel.ImageAttachmentRef, policy ImageRequestPolicy) (string, error) {
	if _, err := attachmentHash(ref); err != nil {
		return "", err
	}
	if err := validateImageRequestPolicy(policy); err != nil {
		return "", err
	}
	descriptor := struct {
		TransformVersion  string              `json:"transformVersion"`
		AttachmentID      string              `json:"attachmentId"`
		RoutePixelBudget  int64               `json:"routePixelBudget"`
		EncodedByteBudget int64               `json:"encodedByteBudget"`
		RequestWidth      int                 `json:"requestWidth,omitempty"`
		RequestHeight     int                 `json:"requestHeight,omitempty"`
		Encoding          requestEncodingSpec `json:"encoding"`
	}{
		TransformVersion: RequestImageTransformVersion, AttachmentID: ref.AttachmentID,
		RoutePixelBudget: policy.MaxPixels, EncodedByteBudget: policy.MaxBytes,
		RequestWidth: policy.Width, RequestHeight: policy.Height,
		Encoding: requestEncodingSpec{PNGAlpha: true, JPEGQualities: requestImageJPEGQualities[:], ColorSpace: "srgb"},
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		return "", newAttachmentError("Unable to identify the request image transform.", "ATTACHMENT_WRITE_FAILED", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type requestEncodingSpec struct {
	PNGAlpha      bool   `json:"pngAlpha"`
	JPEGQualities []int  `json:"jpegQualities"`
	ColorSpace    string `json:"colourspace"`
}

func (s *LocalAttachmentStore) ReadRequestImage(ctx context.Context, ref harnessmodel.ImageAttachmentRef, policy ImageRequestPolicy) (RequestImageAttachment, error) {
	if err := ctx.Err(); err != nil {
		return RequestImageAttachment{}, err
	}
	codec := s.officialCodec.Load()
	variantID, err := RequestImageVariantID(ref, policy)
	if codec != nil {
		variantID, err = codec.variantID(ref, policy)
	}
	if err != nil {
		return RequestImageAttachment{}, err
	}
	if codec != nil {
		codec.used.Store(true)
	}
	s.requestMu.Lock()
	if s.requestInflight == nil {
		s.requestInflight = make(map[string]*sharedRequestImage)
	}
	operation := s.requestInflight[variantID]
	if operation != nil && operation.ctx.Err() != nil {
		delete(s.requestInflight, variantID)
		operation = nil
	}
	if operation == nil {
		operationCtx, cancel := context.WithCancel(context.Background())
		operation = &sharedRequestImage{ctx: operationCtx, cancel: cancel, done: make(chan struct{})}
		s.requestInflight[variantID] = operation
		go s.runRequestImage(operation, ref, policy, variantID, codec)
	}
	operation.waiters++
	s.requestMu.Unlock()

	select {
	case <-operation.done:
		s.releaseRequestImage(operation, false)
		if err := ctx.Err(); err != nil {
			return RequestImageAttachment{}, err
		}
		if codec != nil {
			if err := codec.activeError(); err != nil {
				return RequestImageAttachment{}, err
			}
		} else if s.officialCodec.Load() != nil {
			return RequestImageAttachment{}, newAttachmentError("Image codec changed before request publication.", "ATTACHMENT_WRITE_FAILED", nil)
		}
		return cloneRequestImage(operation.result), operation.err
	case <-ctx.Done():
		s.releaseRequestImage(operation, true)
		return RequestImageAttachment{}, ctx.Err()
	}
}

func (s *LocalAttachmentStore) runRequestImage(operation *sharedRequestImage, ref harnessmodel.ImageAttachmentRef, policy ImageRequestPolicy, variantID string, codec *officialImageCodec) {
	result, err := s.readOrCreateRequestImage(operation.ctx, ref, policy, variantID, codec)
	s.requestMu.Lock()
	operation.result, operation.err, operation.settled = result, err, true
	if s.requestInflight[variantID] == operation {
		delete(s.requestInflight, variantID)
	}
	close(operation.done)
	s.requestMu.Unlock()
}

func (s *LocalAttachmentStore) releaseRequestImage(operation *sharedRequestImage, cancelled bool) {
	s.requestMu.Lock()
	operation.waiters--
	if cancelled && operation.waiters == 0 && !operation.settled {
		operation.cancel()
	}
	s.requestMu.Unlock()
}

func (s *LocalAttachmentStore) readOrCreateRequestImage(
	ctx context.Context,
	ref harnessmodel.ImageAttachmentRef,
	policy ImageRequestPolicy,
	variantID string,
	codec *officialImageCodec,
) (RequestImageAttachment, error) {
	if codec != nil {
		if err := codec.activeError(); err != nil {
			return RequestImageAttachment{}, err
		}
	}
	stored, err := s.ReadImage(ctx, ref)
	if err != nil {
		return RequestImageAttachment{}, err
	}
	if err := ctx.Err(); err != nil {
		return RequestImageAttachment{}, err
	}
	source, err := decodeStoredImage(stored.Data, stored.Ref.MediaType)
	if err != nil {
		return RequestImageAttachment{}, err
	}
	expectedAlpha := inspectImageFacts(stored.Data, stored.Ref.MediaType, source).hasAlpha
	maximumWidth, maximumHeight, err := requestImagePolicyDimensions(ref, policy)
	if err != nil {
		return RequestImageAttachment{}, err
	}
	path, err := s.requestImagePath(variantID)
	if err != nil {
		return RequestImageAttachment{}, err
	}
	if cached, cacheErr := os.ReadFile(path); cacheErr == nil {
		if version, valid := verifyRequestImageBytes(cached, ref, variantID, maximumWidth, maximumHeight, expectedAlpha); valid && (policy.Width == 0 || (version.Width == policy.Width && version.Height == policy.Height)) {
			return version, nil
		}
	} else if !errors.Is(cacheErr, os.ErrNotExist) {
		// An unreadable cache entry is treated like corruption and regenerated;
		// the immutable source remains authoritative.
		_ = os.Remove(path)
	}
	if err := s.acquireCompression(ctx); err != nil {
		return RequestImageAttachment{}, err
	}
	defer s.releaseCompression()
	var created RequestImageAttachment
	if codec != nil {
		created, err = codec.request(ctx, stored, policy, variantID, maximumWidth, maximumHeight, expectedAlpha)
	} else {
		created, err = createRequestImage(stored, source, policy, variantID, maximumWidth, maximumHeight, expectedAlpha)
	}
	if err != nil {
		return RequestImageAttachment{}, err
	}
	if err := ctx.Err(); err != nil {
		return RequestImageAttachment{}, err
	}
	if !bytes.Equal(created.Data, stored.Data) {
		s.codecMu.Lock()
		defer s.codecMu.Unlock()
		if s.officialCodec.Load() != codec {
			return RequestImageAttachment{}, newAttachmentError("Image codec changed before cache publication.", "ATTACHMENT_WRITE_FAILED", nil)
		}
		if codec != nil {
			if err := codec.activeErrorLocked(); err != nil {
				return RequestImageAttachment{}, err
			}
		}
		if err := s.writeRequestImage(path, created.Data); err != nil {
			return RequestImageAttachment{}, err
		}
	}
	return created, nil
}

func createRequestImage(
	stored StoredImageAttachment,
	decoded image.Image,
	policy ImageRequestPolicy,
	variantID string,
	width int,
	height int,
	hasAlpha bool,
) (RequestImageAttachment, error) {
	if width >= stored.Ref.Width && height >= stored.Ref.Height && int64(len(stored.Data)) <= policy.MaxBytes {
		return requestImageVersion(variantID, stored.Ref, stored.Data, stored.Ref.MediaType, stored.Ref.Width, stored.Ref.Height, hasAlpha), nil
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
	mediaType := "image/png"
	var data []byte
	if hasAlpha {
		var output bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&output, alphaPlaneImage{canvas}); err != nil {
			return RequestImageAttachment{}, newAttachmentError("Unable to encode the request image.", "ATTACHMENT_WRITE_FAILED", err)
		}
		data = output.Bytes()
	} else {
		var smallest []byte
		for _, quality := range requestImageJPEGQualities {
			var output bytes.Buffer
			if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: quality}); err != nil {
				return RequestImageAttachment{}, newAttachmentError("Unable to encode the request image.", "ATTACHMENT_WRITE_FAILED", err)
			}
			candidate := output.Bytes()
			if smallest == nil || len(candidate) < len(smallest) {
				smallest = append([]byte(nil), candidate...)
			}
			if int64(len(candidate)) <= policy.MaxBytes {
				data = append([]byte(nil), candidate...)
				break
			}
		}
		if data == nil {
			data = smallest
		}
		mediaType = "image/jpeg"
	}
	version := requestImageVersion(variantID, stored.Ref, data, mediaType, width, height, hasAlpha)
	if verified, valid := verifyRequestImageBytes(version.Data, stored.Ref, variantID, width, height, hasAlpha); !valid || verified.MediaType != mediaType || verified.HasAlpha != hasAlpha {
		return RequestImageAttachment{}, newAttachmentError("Encoded request image failed verification.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	return version, nil
}

func requestImageVersion(
	variantID string,
	ref harnessmodel.ImageAttachmentRef,
	data []byte,
	mediaType string,
	width int,
	height int,
	hasAlpha bool,
) RequestImageAttachment {
	return RequestImageAttachment{
		VariantID: variantID, Attachment: ref, Data: append([]byte(nil), data...), MediaType: mediaType,
		Bytes: int64(len(data)), Width: width, Height: height, Depth: "uchar", Space: "srgb", HasAlpha: hasAlpha,
	}
}

func verifyRequestImageBytes(
	data []byte,
	ref harnessmodel.ImageAttachmentRef,
	variantID string,
	maximumWidth int,
	maximumHeight int,
	expectedAlpha bool,
) (RequestImageAttachment, bool) {
	width, height, mediaType, err := probeImageMetadata(data)
	if err != nil || width <= 0 || height <= 0 || width > maximumWidth || height > maximumHeight {
		return RequestImageAttachment{}, false
	}
	decoded, err := decodeStoredImage(data, mediaType)
	if err != nil {
		return RequestImageAttachment{}, false
	}
	facts := inspectImageFacts(data, mediaType, decoded)
	if facts.depth != "uchar" || facts.space != "srgb" || facts.metadata || facts.animated {
		return RequestImageAttachment{}, false
	}
	hasAlpha := facts.hasAlpha
	if hasAlpha != expectedAlpha && !(expectedAlpha && !hasAlpha && mediaType == "image/webp") {
		return RequestImageAttachment{}, false
	}
	return requestImageVersion(variantID, ref, data, mediaType, width, height, hasAlpha), true
}

func decodeStoredImage(data []byte, mediaType string) (image.Image, error) {
	reader := bytes.NewReader(data)
	var decoded image.Image
	var err error
	switch mediaType {
	case "image/png":
		decoded, err = png.Decode(reader)
	case "image/jpeg":
		decoded, err = jpeg.Decode(reader)
	case "image/gif":
		var animation *gif.GIF
		animation, err = gif.DecodeAll(reader)
		if err == nil {
			if len(animation.Image) == 0 {
				err = errors.New("GIF has no frames")
			} else {
				decoded = animation.Image[0]
			}
		}
	case "image/webp":
		decoded, err = webp.Decode(reader)
	default:
		err = errors.New("unsupported image type")
	}
	if err != nil {
		return nil, newAttachmentError("Unable to decode the stored image.", "ATTACHMENT_CORRUPT", err)
	}
	return decoded, nil
}

func imageHasAlphaChannel(source image.Image) bool {
	switch typed := source.(type) {
	case *image.Alpha, *image.Alpha16, *image.NRGBA, *image.NRGBA64, *image.RGBA, *image.RGBA64:
		return true
	case *image.Paletted:
		for _, colour := range typed.Palette {
			_, _, _, alpha := colour.RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

func validateImageRequestPolicy(policy ImageRequestPolicy) error {
	if policy.MaxPixels <= 0 || policy.MaxBytes <= 0 || policy.Width < 0 || policy.Height < 0 || (policy.Width == 0) != (policy.Height == 0) {
		return newAttachmentError("Image request policy requires positive integer limits.", "INVALID_ATTACHMENT_REF", nil)
	}
	return nil
}

func requestImagePolicyDimensions(ref harnessmodel.ImageAttachmentRef, policy ImageRequestPolicy) (int, int, error) {
	if policy.Width > 0 && policy.Height > 0 {
		if policy.Width > ref.Width || policy.Height > ref.Height || int64(policy.Width) > policy.MaxPixels/int64(policy.Height) {
			return 0, 0, newAttachmentError("Request image target must not enlarge the source or exceed its pixel budget.", "INVALID_ATTACHMENT_REF", nil)
		}
		return policy.Width, policy.Height, nil
	}
	return RequestImageDimensions(ref.Width, ref.Height, policy.MaxPixels)
}

func (s *LocalAttachmentStore) requestImagePath(variantID string) (string, error) {
	match := attachmentIDPattern.FindStringSubmatch(variantID)
	if len(match) != 2 {
		return "", newAttachmentError("Request image variant is invalid.", "INVALID_ATTACHMENT_REF", nil)
	}
	return filepath.Join(s.root, "request-images", match[1][:2], match[1]), nil
}

func (s *LocalAttachmentStore) writeRequestImage(path string, data []byte) error {
	directory := filepath.Dir(path)
	for _, target := range []string{filepath.Join(s.root, "request-images"), directory} {
		if err := ensurePrivateDirectory(target); err != nil {
			return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
		}
	}
	temporary, err := os.CreateTemp(directory, "pending-*")
	if err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := temporary.Sync(); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := temporary.Close(); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := syncDirectory(directory); err != nil {
		return newAttachmentError("Unable to persist the request image.", "ATTACHMENT_WRITE_FAILED", err)
	}
	return nil
}

func cloneRequestImage(source RequestImageAttachment) RequestImageAttachment {
	source.Data = append([]byte(nil), source.Data...)
	source.Attachment = harnessmodel.CloneImageAttachmentRef(source.Attachment)
	return source
}

var _ RequestImageStore = (*LocalAttachmentStore)(nil)
