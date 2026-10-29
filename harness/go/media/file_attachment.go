// Contract algorithms ported from DeepSeek DSH (MIT); see
// go/harness/DEEPSEEK_FILES_LICENSE.txt for the retained notice.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

type SaveFileAttachment struct {
	Data []byte
	Name string
}
type SaveFileStreamAttachment struct {
	Data io.Reader
	Name string
}
type EncodedFileAttachment struct {
	Data string `json:"data"`
	Name string `json:"name,omitempty"`
}

// FileAttachmentStore is optional so existing image-only deployments fail
// closed on files without breaking their image interface.
type FileAttachmentStore interface {
	SaveFile(context.Context, SaveFileAttachment) (harnessmodel.FileAttachmentRef, error)
	SaveFileStream(context.Context, SaveFileStreamAttachment) (harnessmodel.FileAttachmentRef, error)
	ReadFileStream(context.Context, harnessmodel.FileAttachmentRef) (io.ReadCloser, error)
}
type FileHostPathStore interface {
	FileHostPath(harnessmodel.FileAttachmentRef) (string, error)
}

var windowsFileDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)

// FileLeafName removes both clients' path separators and produces the same
// cross-platform stored leaf as the official attachment-local service.
func FileLeafName(value string) string {
	value = value[strings.LastIndexAny(value, `/\`)+1:]
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		if strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
	value = strings.TrimRight(strings.TrimFunc(value, fileNameWhitespace), ". ")
	stem := value
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if windowsFileDevice.MatchString(strings.TrimRight(stem, ". ")) {
		value = "_" + value
	}
	if len(value) > 255 {
		end := 255
		for !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end]
	}
	value = strings.TrimRight(value, ". ")
	if value == "" || value == "." || value == ".." {
		return "file"
	}
	return value
}

// Match ECMAScript String.trim, whose set differs from Go at U+0085/U+FEFF.
func fileNameWhitespace(r rune) bool {
	return r >= 0x09 && r <= 0x0d || r == 0x20 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
}

func ValidateFileAttachmentRef(ref harnessmodel.FileAttachmentRef) error {
	if !attachmentIDPattern.MatchString(ref.AttachmentID) || ref.Name != FileLeafName(ref.Name) || ref.Bytes < 0 {
		return newAttachmentError("File attachment reference is invalid.", "INVALID_ATTACHMENT_REF", nil)
	}
	return nil
}

func (s *LocalAttachmentStore) FileHostPath(ref harnessmodel.FileAttachmentRef) (string, error) {
	if err := ValidateFileAttachmentRef(ref); err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(ref.AttachmentID, "sha256:")
	return filepath.Join(s.root, "files", digest[:2], digest, ref.Name), nil
}

func AdmitEncodedFile(ctx context.Context, store FileAttachmentStore, input EncodedFileAttachment) (harnessmodel.FileAttachmentRef, error) {
	if err := ctx.Err(); err != nil {
		return harnessmodel.FileAttachmentRef{}, err
	}
	data, err := base64.StdEncoding.Strict().DecodeString(input.Data)
	if err != nil || base64.StdEncoding.EncodeToString(data) != input.Data {
		return harnessmodel.FileAttachmentRef{}, newAttachmentError("File upload is not canonical base64.", "INVALID_FILE_BASE64", err)
	}
	return store.SaveFile(ctx, SaveFileAttachment{Data: data, Name: input.Name})
}

func (s *LocalAttachmentStore) SaveFile(ctx context.Context, input SaveFileAttachment) (harnessmodel.FileAttachmentRef, error) {
	return s.SaveFileStream(ctx, SaveFileStreamAttachment{Data: bytes.NewReader(input.Data), Name: input.Name})
}

// fileWriteFailure preserves actual I/O failures that coincide with
// cancellation; a cancellation signal alone is not a successful fsync/cleanup.
func fileWriteFailure(ctx context.Context, err error) error {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return err
	}
	var attachmentErr *AttachmentError
	if !errors.As(err, &attachmentErr) {
		err = newAttachmentError("Unable to persist file attachment.", "ATTACHMENT_WRITE_FAILED", err)
	}
	if cause := ctx.Err(); cause != nil && !errors.Is(err, cause) {
		return errors.Join(cause, err)
	}
	return err
}

func (s *LocalAttachmentStore) SaveFileStream(ctx context.Context, input SaveFileStreamAttachment) (resultRef harnessmodel.FileAttachmentRef, resultErr error) {
	fail := func(err error) (harnessmodel.FileAttachmentRef, error) {
		return harnessmodel.FileAttachmentRef{}, fileWriteFailure(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fail(err)
	}
	temp, err := os.CreateTemp(s.root, ".file-upload-*")
	if err != nil {
		return fail(err)
	}
	closeAttempted := false
	defer func() {
		var cleanupErr error
		if !closeAttempted {
			cleanupErr = temp.Close()
		}
		if err := os.Remove(temp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
		if cleanupErr != nil {
			resultRef = harnessmodel.FileAttachmentRef{}
			resultErr = errors.Join(resultErr, fileWriteFailure(ctx, cleanupErr))
		}
	}()
	digest := sha256.New()
	reader := input.Data
	if reader == nil {
		reader = bytes.NewReader(nil)
	}
	count, err := io.CopyBuffer(io.MultiWriter(temp, digest), &fileContextReader{ctx: ctx, reader: reader}, make([]byte, 64<<10))
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := temp.Sync(); err != nil {
		return fail(err)
	}
	closeAttempted = true
	if err := temp.Close(); err != nil {
		return fail(err)
	}
	// Serialize publication, not streaming, so a concurrent alias cannot make
	// the shared inode read-only before its staging link is removed on Windows.
	s.filePublishMu.Lock()
	defer s.filePublishMu.Unlock()
	hexDigest := hex.EncodeToString(digest.Sum(nil))
	ref := harnessmodel.FileAttachmentRef{AttachmentID: "sha256:" + hexDigest, Name: FileLeafName(input.Name), Bytes: count}
	objectPath := filepath.Join(s.root, "file-objects", hexDigest[:2], hexDigest)
	if err := publishFileAlias(temp.Name(), objectPath, ref); err != nil {
		return fail(err)
	}
	if err := os.Remove(temp.Name()); err != nil {
		return fail(err)
	}
	if err := os.Chmod(objectPath, 0o444); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	path, _ := s.FileHostPath(ref)
	if err := publishFileAlias(objectPath, path, ref); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return ref, nil
}

// Hard-link publication never overwrites an existing object. Every collision
// is verified. POSIX also syncs the directory; Windows retains the synced
// object but cannot promise directory-entry power-loss durability.
func publishFileAlias(source, target string, ref harnessmodel.FileAttachmentRef) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Link(source, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := verifyFileObject(target, ref); err != nil {
			return err
		}
	}
	return syncDirectory(filepath.Dir(target))
}

func verifyFileObject(path string, ref harnessmodel.FileAttachmentRef) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != ref.Bytes {
		return newAttachmentError("Stored file attachment failed integrity verification.", "ATTACHMENT_CORRUPT", nil)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.CopyBuffer(digest, file, make([]byte, 64<<10)); err != nil {
		return err
	}
	if "sha256:"+hex.EncodeToString(digest.Sum(nil)) != ref.AttachmentID {
		return newAttachmentError("Stored file attachment failed integrity verification.", "ATTACHMENT_CORRUPT", nil)
	}
	return nil
}

type fileContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *fileContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cause := r.ctx.Err(); cause != nil {
		if err == nil || err == io.EOF {
			return n, cause
		}
		return n, errors.Join(cause, err)
	}
	return n, err
}

type verifiedFileReader struct {
	file  *os.File
	ctx   context.Context
	ref   harnessmodel.FileAttachmentRef
	hash  io.Writer
	sum   func() []byte
	count int64
	done  bool
}

func (r *verifiedFileReader) Close() error { return r.file.Close() }

func (r *verifiedFileReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.done {
		return 0, io.EOF
	}
	n, err := r.file.Read(p)
	if cause := r.ctx.Err(); cause != nil {
		// Closing this exact file is how its Host owner interrupts a blocked
		// read. Do not turn that expected close into a false cleanup failure;
		// retain an independent I/O failure if one coincides with cancellation.
		if err == nil || err == io.EOF || errors.Is(err, os.ErrClosed) {
			return n, cause
		}
		return n, errors.Join(cause, err)
	}
	if n > 0 {
		_, _ = r.hash.Write(p[:n])
		r.count += int64(n)
	}
	if err == io.EOF {
		r.done = true
		if r.count != r.ref.Bytes || "sha256:"+hex.EncodeToString(r.sum()) != r.ref.AttachmentID {
			return n, newAttachmentError("Stored file attachment failed integrity verification.", "ATTACHMENT_CORRUPT", nil)
		}
	} else if err != nil {
		return n, newAttachmentError("Unable to read file attachment.", "ATTACHMENT_READ_FAILED", err)
	}
	return n, err
}
func (s *LocalAttachmentStore) ReadFileStream(ctx context.Context, ref harnessmodel.FileAttachmentRef) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.FileHostPath(ref)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, newAttachmentError("File attachment object is missing.", "ATTACHMENT_NOT_FOUND", err)
	}
	if err != nil {
		return nil, newAttachmentError("Unable to read file attachment.", "ATTACHMENT_READ_FAILED", err)
	}
	if !info.Mode().IsRegular() {
		return nil, newAttachmentError("Stored file attachment failed integrity verification.", "ATTACHMENT_CORRUPT", fmt.Errorf("object is not a regular file"))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, newAttachmentError("Unable to read file attachment.", "ATTACHMENT_READ_FAILED", err)
	}
	hash := sha256.New()
	return &verifiedFileReader{file: file, ctx: ctx, ref: ref, hash: hash, sum: func() []byte { return hash.Sum(nil) }}, nil
}
