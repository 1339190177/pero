package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func TestFileAttachmentStreamVerbatimAndConcurrentAliases(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("binary\x00file\xff"), 10000)
	var wg sync.WaitGroup
	refs := make(chan harnessmodel.FileAttachmentRef, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, err := store.SaveFileStream(t.Context(), SaveFileStreamAttachment{Data: bytes.NewReader(data), Name: `C:\private\CON.txt`})
			if err != nil {
				t.Error(err)
				return
			}
			refs <- ref
		}()
	}
	wg.Wait()
	close(refs)
	for ref := range refs {
		if ref.Name != "_CON.txt" || ref.Bytes != int64(len(data)) {
			t.Fatalf("ref=%+v", ref)
		}
		stream, err := store.ReadFileStream(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(stream)
		stream.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("bytes=%d err=%v", len(got), err)
		}
		path, _ := store.FileHostPath(ref)
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("mode=%v err=%v", info, err)
		}
	}
	empty, err := AdmitEncodedFile(t.Context(), store, EncodedFileAttachment{Data: ""})
	if err != nil || empty.Bytes != 0 || empty.Name != "file" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	for _, data := range []string{"Zg", "Zh==", "Zg==\n", "%%%", " A==="} {
		if _, err := AdmitEncodedFile(t.Context(), store, EncodedFileAttachment{Data: data}); err == nil {
			t.Fatalf("accepted noncanonical %q", data)
		}
	}
}

type failingFileReader struct{}

func (failingFileReader) Read([]byte) (int, error) { return 0, errors.New("source failed") }
func TestFileAttachmentCancellationFailureIntegrityAndCleanup(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.SaveFileStream(ctx, SaveFileStreamAttachment{Data: bytes.NewReader([]byte("no"))}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.SaveFileStream(t.Context(), SaveFileStreamAttachment{Data: failingFileReader{}}); err == nil {
		t.Fatal("source failure accepted")
	}
	leftovers, _ := filepath.Glob(filepath.Join(store.Root(), ".file-upload-*"))
	if len(leftovers) != 0 {
		t.Fatalf("leaked temps %v", leftovers)
	}
	ref, err := store.SaveFile(t.Context(), SaveFileAttachment{Data: []byte("test"), Name: "report.txt"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := ref
	invalid.Name = "../../escape"
	if _, err := store.FileHostPath(invalid); err == nil {
		t.Fatal("path authority accepted")
	}
	wrong := ref
	wrong.Bytes++
	reader, err := store.ReadFileStream(t.Context(), wrong)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, reader)
	reader.Close()
	var attachment *AttachmentError
	if !errors.As(err, &attachment) || attachment.Code != "ATTACHMENT_CORRUPT" {
		t.Fatalf("integrity err=%v", err)
	}
}
func TestFileContentWireCloneAndProviderProjection(t *testing.T) {
	ref := harnessmodel.FileAttachmentRef{AttachmentID: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "报告.txt", Bytes: 3}
	message := harnessmodel.Message{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{{Type: harnessmodel.BlockFile, FileAttachment: &ref}}}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded harnessmodel.Message
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Content[0].Attachment != nil || decoded.Content[0].FileAttachment == nil || *decoded.Content[0].FileAttachment != ref {
		t.Fatalf("wire=%s", raw)
	}
	projected, err := ProjectFilesToText([]harnessmodel.Message{decoded}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if projected[0].Content[0].Type != harnessmodel.BlockText || projected[0].Content[0].Text != harnessmodel.FileHandleText(ref, "") {
		t.Fatal("file was dropped or changed")
	}
	clone := harnessmodel.CloneMessage(decoded)
	clone.Content[0].FileAttachment.Name = "changed"
	if decoded.Content[0].FileAttachment.Name != ref.Name {
		t.Fatal("clone aliased file")
	}
}

// Content annotations and literal aliases never enter the typed store API.
// Use real immutable filesystem objects, not a second lookup implementation.
func TestContentAttachmentExtrasKeepCanonicalStorageAuthority(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SaveFile(t.Context(), SaveFileAttachment{Data: []byte("canonical bytes"), Name: "canonical.txt"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.SaveFile(t.Context(), SaveFileAttachment{Data: []byte("other bytes"), Name: "other.txt"})
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(first)
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(canonical, &fields); err != nil {
		t.Fatal(err)
	}
	fields["AttachmentID"], _ = json.Marshal(other.AttachmentID)
	fields["Name"], _ = json.Marshal(other.Name)
	fields["Bytes"], _ = json.Marshal(other.Bytes)
	fields["annotation"] = json.RawMessage(`{"path":"../../not-authority","negativeZero":-0,"raw":"\ud800"}`)
	encoded, _ := json.Marshal(fields)
	var block harnessmodel.ContentBlock
	if err = json.Unmarshal(append(append([]byte(`{"type":"file","attachment":`), encoded...), '}'), &block); err != nil {
		t.Fatal(err)
	}
	if *block.FileAttachment != first {
		t.Fatalf("literal aliases changed file authority: %+v", block.FileAttachment)
	}
	read := func(ref harnessmodel.FileAttachmentRef) []byte {
		reader, err := store.ReadFileStream(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if got := read(*block.FileAttachment); string(got) != "canonical bytes" {
		t.Fatalf("read wrong object: %q", got)
	}
	block.FileAttachment = &other
	roundtrip, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(roundtrip, []byte(`"annotation"`)) || bytes.Contains(roundtrip, []byte(`"AttachmentID"`)) {
		t.Fatal("replacement acquired old raw metadata")
	}
	if got := read(*block.FileAttachment); string(got) != "other bytes" {
		t.Fatalf("replacement read wrong object: %q", got)
	}
	delete(fields, "attachmentId")
	encoded, _ = json.Marshal(fields)
	if err = json.Unmarshal(append(append([]byte(`{"type":"file","attachment":`), encoded...), '}'), &block); err != nil {
		t.Fatal(err)
	}
	if block.FileAttachment.AttachmentID != "" {
		t.Fatal("alias supplied missing canonical ID")
	}
	if reader, err := store.ReadFileStream(t.Context(), *block.FileAttachment); err == nil {
		reader.Close()
		t.Fatal("alias-only content resolved a stored file")
	}
}

func TestImageAttachmentExtrasKeepCanonicalStorageAuthority(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SaveImage(t.Context(), SaveImageAttachment{Data: testPNG(t, 1, 1), MediaType: "image/png", Name: "first.png"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.SaveImage(t.Context(), SaveImageAttachment{Data: testPNG(t, 2, 1), MediaType: "image/png", Name: "other.png"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first)
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["AttachmentID"], _ = json.Marshal(other.AttachmentID)
	fields["MediaType"] = json.RawMessage(`"image/jpeg"`)
	fields["Width"] = json.RawMessage(`999`)
	fields["annotation"] = json.RawMessage(`{"path":"../../not-authority","raw":"\ud800"}`)
	encoded, _ := json.Marshal(fields)
	var block harnessmodel.ContentBlock
	if err = json.Unmarshal(append(append([]byte(`{"type":"image","attachment":`), encoded...), '}'), &block); err != nil {
		t.Fatal(err)
	}
	if *block.Attachment != first {
		t.Fatalf("literal aliases changed image authority: %+v", block.Attachment)
	}
	got, err := store.ReadImage(t.Context(), *block.Attachment)
	if err != nil || got.Ref.AttachmentID != first.AttachmentID {
		t.Fatalf("read wrong image: %+v %v", got.Ref, err)
	}
	block.Attachment = &other
	roundtrip, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(roundtrip, []byte(`"annotation"`)) || bytes.Contains(roundtrip, []byte(`"AttachmentID"`)) {
		t.Fatal("replacement image acquired old raw metadata")
	}
	got, err = store.ReadImage(t.Context(), *block.Attachment)
	if err != nil || got.Ref.AttachmentID != other.AttachmentID {
		t.Fatalf("replacement read wrong image: %+v %v", got.Ref, err)
	}
	delete(fields, "attachmentId")
	encoded, _ = json.Marshal(fields)
	if err = json.Unmarshal(append(append([]byte(`{"type":"image","attachment":`), encoded...), '}'), &block); err != nil {
		t.Fatal(err)
	}
	if block.Attachment.AttachmentID != "" {
		t.Fatal("alias supplied missing canonical image ID")
	}
	if _, err = store.ReadImage(t.Context(), *block.Attachment); err == nil {
		t.Fatal("alias-only content resolved stored image")
	}
}
