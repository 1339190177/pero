package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

//go:embed official_image_codec.mjs
var officialImageCodecProgram string

// OfficialImageCodecConfig is trusted deployment configuration, never a tool,
// HTTP, or prompt argument. Anchor must be an explicit installed package.json
// whose dependency graph owns @deepseek-ai/dsh-attachment-local.
type OfficialImageCodecConfig struct {
	Node           string
	Anchor         string
	Timeout        time.Duration
	MaxInputBytes  int64
	MaxOutputBytes int64
}

type officialImageCodec struct {
	config   OfficialImageCodecConfig
	identity string
	owner    context.Context
	cancel   context.CancelFunc
	used     atomic.Bool
	previous *officialImageCodec
	store    *LocalAttachmentStore
	owners   map[*int]context.Context // guarded by store.codecMu
}

type officialCodecReply struct {
	Identity string                          `json:"identity"`
	Data     string                          `json:"data"`
	Ref      harnessmodel.ImageAttachmentRef `json:"ref"`
	RequestImageAttachment
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// BindOfficialImageCodec enables the actual deployed DSH/Sharp transforms.
// It fails closed when the package is missing/invalid and never falls back to
// the native codec after binding. Owner cancellation retires this binding.
func (s *LocalAttachmentStore) BindOfficialImageCodec(ctx context.Context, config OfficialImageCodecConfig) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(config.Anchor) || filepath.Base(config.Anchor) != "package.json" {
		return nil, errors.New("media: official image codec requires an absolute deployment package.json anchor")
	}
	anchor, err := filepath.EvalSymlinks(config.Anchor)
	if err != nil {
		return nil, fmt.Errorf("media: resolve official image codec anchor: %w", err)
	}
	info, err := os.Stat(anchor)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("media: official image codec anchor is not a regular file")
	}
	config.Anchor = anchor
	if config.Node == "" {
		config.Node = "node"
	}
	config.Node, err = exec.LookPath(config.Node)
	if err != nil {
		return nil, fmt.Errorf("media: official image codec Node unavailable: %w", err)
	}
	config.Node, err = filepath.Abs(config.Node)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxInputBytes == 0 {
		config.MaxInputBytes = s.limits.MaxImageBytes
	}
	if config.MaxOutputBytes == 0 {
		config.MaxOutputBytes = s.limits.MaxImageBytes
	}
	if config.MaxInputBytes <= 0 || config.MaxOutputBytes <= 0 || config.MaxInputBytes > 256<<20 || config.MaxOutputBytes > 256<<20 {
		return nil, errors.New("media: official image codec byte bounds must be within 1..256 MiB")
	}
	codec := &officialImageCodec{config: config, owner: ctx}
	reply, err := codec.call(ctx, map[string]any{"operation": "identify"})
	if err != nil {
		return nil, err
	}
	if decoded, err := hex.DecodeString(reply.Identity); err != nil || len(decoded) != 32 {
		return nil, errors.New("media: invalid official image codec identity")
	}
	codec.identity = reply.Identity
	s.codecMu.Lock()
	previous := s.officialCodec.Load()
	if previous != nil && previous.owner.Err() == nil {
		if previous.identity != codec.identity || previous.config != codec.config {
			s.codecMu.Unlock()
			return nil, errors.New("media: official image codec is already bound to a different live installation or policy")
		}
		codec = previous
	} else {
		codec.owner, codec.cancel = context.WithCancel(context.Background())
		codec.previous = previous
		codec.store = s
		codec.owners = make(map[*int]context.Context)
		s.officialCodec.Store(codec)
		s.codecLeases = 0
	}
	s.codecLeases++
	lease := new(int)
	codec.owners[lease] = ctx
	s.codecMu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.codecMu.Lock()
			defer s.codecMu.Unlock()
			if s.officialCodec.Load() == codec {
				delete(codec.owners, lease)
				s.codecLeases--
				if s.codecLeases == 0 {
					codec.cancel()
					if !codec.used.Load() {
						s.officialCodec.Store(codec.previous)
					}
				}
			}
		})
	}
	stop := context.AfterFunc(ctx, release)
	if err := ctx.Err(); err != nil {
		stop()
		release()
		return nil, err
	}
	return func() { stop(); release() }, nil
}

func (c *officialImageCodec) activeErrorLocked() error {
	if err := c.owner.Err(); err != nil {
		return err
	}
	if c.store == nil {
		return nil
	}
	if c.store.officialCodec.Load() != c {
		return context.Canceled
	}
	for _, owner := range c.owners {
		if owner.Err() == nil {
			return nil
		}
	}
	return context.Canceled
}

func (c *officialImageCodec) activeError() error {
	if c.store == nil {
		return c.owner.Err()
	}
	c.store.codecMu.Lock()
	defer c.store.codecMu.Unlock()
	return c.activeErrorLocked()
}

// OfficialImageCodecIdentity is a capability fact for trusted Host composition.
// The salt covers carrier version, installed module bytes and Sharp/libvips versions.
func (s *LocalAttachmentStore) OfficialImageCodecIdentity() string {
	if codec := s.officialCodec.Load(); codec != nil {
		return codec.identity
	}
	return ""
}

func (c *officialImageCodec) call(ctx context.Context, input any) (officialCodecReply, error) {
	var reply officialCodecReply
	if err := ctx.Err(); err != nil {
		return reply, err
	}
	if err := c.activeError(); err != nil {
		return reply, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	stop := context.AfterFunc(c.owner, cancel)
	defer stop()
	encoded, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if int64(len(encoded)) > int64(base64.StdEncoding.EncodedLen(int(c.config.MaxInputBytes)))+int64(64<<10) {
		return reply, newAttachmentError("Image codec input exceeds its byte limit.", "IMAGE_TOO_LARGE", nil)
	}
	scratch, err := os.MkdirTemp("", "point-image-codec-*")
	if err != nil {
		return reply, err
	}
	defer os.RemoveAll(scratch)
	command := exec.CommandContext(ctx, c.config.Node, "--input-type=module", "--eval", officialImageCodecProgram, c.config.Anchor, scratch, c.identity)
	command.Dir = scratch
	command.Env = imageCodecEnvironment(os.Environ(), scratch)
	command.Stdin = bytes.NewReader(encoded)
	command.WaitDelay = time.Second
	configureImageCodecProcess(command)
	output := &imageCodecBuffer{limit: int64(base64.StdEncoding.EncodedLen(int(c.config.MaxOutputBytes))) + 64<<10, cancel: cancel}
	diagnostics := &imageCodecBuffer{limit: 16 << 10, cancel: cancel}
	command.Stdout, command.Stderr = output, diagnostics
	if err := command.Start(); err != nil {
		return reply, newAttachmentError("Official image codec could not start.", "ATTACHMENT_WRITE_FAILED", err)
	}
	err = command.Wait()
	cleanupImageCodecProcess(command)
	if output.exceeded || diagnostics.exceeded {
		return reply, newAttachmentError("Official image codec exceeded its output limit.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	if ctx.Err() != nil {
		return reply, ctx.Err()
	}
	if err := c.activeError(); err != nil {
		return reply, err
	}
	if err != nil {
		return reply, newAttachmentError("Official image codec failed; check the installed attachment package and Sharp runtime.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if err := json.Unmarshal(output.buffer.Bytes(), &reply); err != nil {
		return reply, newAttachmentError("Official image codec returned an invalid result.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if c.identity != "" && reply.Identity != c.identity {
		return reply, newAttachmentError("Official image codec identity changed.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	if reply.Error != nil {
		return reply, newAttachmentError(reply.Error.Message, reply.Error.Code, nil)
	}
	return reply, nil
}

func imageCodecEnvironment(environment []string, scratch string) []string {
	// No credentials, Node loader hooks, dynamic-library injection or arbitrary
	// inherited application settings enter this bytes-only codec process.
	result := []string{"HOME=" + scratch, "TMPDIR=" + scratch, "TMP=" + scratch, "TEMP=" + scratch, "VIPS_CONCURRENCY=1", "UV_THREADPOOL_SIZE=1"}
	for _, value := range environment {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(key) {
		case "PATH", "SYSTEMROOT", "WINDIR", "LANG", "LC_ALL":
			result = append(result, value)
		}
	}
	return result
}

type imageCodecBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
	cancel   context.CancelFunc
}

func (b *imageCodecBuffer) Write(data []byte) (int, error) {
	if int64(len(data)) > b.limit-int64(b.buffer.Len()) {
		b.exceeded = true
		b.cancel()
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(data)
}

func (c *officialImageCodec) prepare(ctx context.Context, input SaveImageAttachment, limits LocalAttachmentStoreConfig) (preparedImageAttachment, error) {
	c.used.Store(true)
	if int64(len(input.Data)) > c.config.MaxInputBytes {
		return preparedImageAttachment{}, newAttachmentError("Image exceeds the codec input limit.", "IMAGE_TOO_LARGE", nil)
	}
	reply, err := c.call(ctx, map[string]any{
		"operation": "prepare", "data": input.Data, "mediaType": input.MediaType, "name": input.Name,
		"limits": map[string]any{"maxImageBytes": limits.MaxImageBytes, "maxImagePixels": limits.MaxImagePixels, "maxImageDimension": limits.MaxImageDimension},
		"policy": map[string]any{"maxPixels": limits.NormalizedImageMaxPixels, "maxDimension": limits.NormalizedImageMaxDimension, "maxBytes": limits.NormalizedImageMaxBytes},
	})
	if err != nil {
		return preparedImageAttachment{}, err
	}
	data, err := c.decode(reply)
	if err != nil {
		return preparedImageAttachment{}, err
	}
	stored := StoredImageAttachment{Ref: reply.Ref, Data: data}
	if err := VerifyStoredImageAttachment(stored); err != nil {
		return preparedImageAttachment{}, err
	}
	if reply.Ref.Width <= 0 || reply.Ref.Height <= 0 || reply.Ref.Width > limits.NormalizedImageMaxDimension || reply.Ref.Height > limits.NormalizedImageMaxDimension ||
		int64(reply.Ref.Width) > limits.NormalizedImageMaxPixels/int64(reply.Ref.Height) {
		return preparedImageAttachment{}, newAttachmentError("Official image codec exceeded normalized dimension limits.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	decoded, err := decodeStoredImage(data, reply.Ref.MediaType)
	if err != nil {
		return preparedImageAttachment{}, err
	}
	facts := inspectImageFacts(data, reply.Ref.MediaType, decoded)
	if facts.metadata || facts.animated || facts.depth != "uchar" || facts.space != "srgb" {
		return preparedImageAttachment{}, newAttachmentError("Official image codec returned invalid normalized facts.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	return preparedImageAttachment{ref: reply.Ref, data: data, codec: c}, nil
}

func (c *officialImageCodec) decode(reply officialCodecReply) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(reply.Data)
	if err != nil || len(data) == 0 || int64(len(data)) > c.config.MaxOutputBytes || base64.StdEncoding.EncodeToString(data) != reply.Data {
		return nil, newAttachmentError("Official image codec returned invalid image bytes.", "ATTACHMENT_WRITE_FAILED", err)
	}
	return data, nil
}

func (c *officialImageCodec) variantID(ref harnessmodel.ImageAttachmentRef, policy ImageRequestPolicy) (string, error) {
	if _, err := attachmentHash(ref); err != nil {
		return "", err
	}
	if err := validateImageRequestPolicy(policy); err != nil {
		return "", err
	}
	width, height, err := requestImagePolicyDimensions(ref, policy)
	if err != nil {
		return "", err
	}
	// Native upload/cache identities additionally bind the actual installed
	// encoder. They must never alias portable PNG/CatmullRom output or a different
	// Sharp/libvips build under the official algorithm-only variant ID.
	descriptor, err := json.Marshal(struct {
		CodecIdentity    string `json:"codecIdentity"`
		TransformVersion string `json:"transformVersion"`
		AttachmentID     string `json:"attachmentId"`
		Width            int    `json:"targetWidth"`
		Height           int    `json:"targetHeight"`
		MaxBytes         int64  `json:"encodedByteBudget"`
	}{c.identity, "request-image-v6", ref.AttachmentID, width, height, policy.MaxBytes})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(descriptor)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (c *officialImageCodec) request(ctx context.Context, stored StoredImageAttachment, policy ImageRequestPolicy, variantID string, width, height int, expectedAlpha bool) (RequestImageAttachment, error) {
	c.used.Store(true)
	reply, err := c.call(ctx, map[string]any{
		"operation": "request", "data": stored.Data, "ref": stored.Ref,
		"target": map[string]any{"width": width, "height": height, "maxBytes": policy.MaxBytes},
	})
	if err != nil {
		return RequestImageAttachment{}, err
	}
	data, err := c.decode(reply)
	if err != nil {
		return RequestImageAttachment{}, err
	}
	version, valid := verifyRequestImageBytes(data, stored.Ref, variantID, width, height, expectedAlpha)
	if !valid || version.Width != reply.Width || version.Height != reply.Height || version.MediaType != reply.MediaType || version.HasAlpha != reply.HasAlpha || version.Bytes != reply.Bytes || reply.Depth != "uchar" || reply.Space != "srgb" {
		return RequestImageAttachment{}, newAttachmentError("Official image codec request bytes failed verification.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	return version, nil
}
