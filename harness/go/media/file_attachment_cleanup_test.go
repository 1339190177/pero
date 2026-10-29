package media

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

type cancelFailFileReader struct {
	cancel context.CancelFunc
	err    error
}

func (r cancelFailFileReader) Read([]byte) (int, error) { r.cancel(); return 0, r.err }

func TestFileWriteCancellationPreservesConcurrentIOFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	diskErr := errors.New("native file sync failed")
	for _, failure := range []error{diskErr, errors.Join(context.Canceled, diskErr)} {
		err := fileWriteFailure(ctx, failure)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, diskErr) {
			t.Fatalf("cancellation masked real native failure: %v", err)
		}
	}
	if err := fileWriteFailure(ctx, context.Canceled); err != context.Canceled {
		t.Fatalf("plain cancellation changed: %v", err)
	}
	local, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	readCtx, stop := context.WithCancel(t.Context())
	defer stop()
	_, err = local.SaveFileStream(readCtx, SaveFileStreamAttachment{Name: "cancel.bin", Data: cancelFailFileReader{cancel: stop, err: diskErr}})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, diskErr) {
		t.Fatalf("native stream lost concurrent I/O failure: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(local.Root(), ".file-upload-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("native return preceded temp cleanup: %v %v", leftovers, err)
	}
}

func TestFileContextReaderDoesNotTurnCancelledEOFIntoCleanupFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &fileContextReader{ctx: ctx, reader: cancelFailFileReader{cancel: cancel, err: io.EOF}}
	_, err := reader.Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		t.Fatalf("cancelled EOF became an I/O failure: %v", err)
	}
}
