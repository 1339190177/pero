package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func TestRequestImageDimensionsMatchDSHProjection(t *testing.T) {
	tests := []struct {
		width, height int
		maxPixels     int64
		wantWidth     int
		wantHeight    int
	}{
		{4096, 4096, 640_000, 800, 800},
		{4096, 2048, 640_000, 1130, 565},
		{3840, 2160, 640_000, 1066, 600},
		{2160, 3840, 640_000, 600, 1066},
		{2, 4, 5, 1, 2},
	}
	for _, test := range tests {
		width, height, err := RequestImageDimensions(test.width, test.height, test.maxPixels)
		if err != nil || width != test.wantWidth || height != test.wantHeight {
			t.Fatalf("RequestImageDimensions(%d, %d, %d) = %dx%d, %v", test.width, test.height, test.maxPixels, width, height, err)
		}
	}
}

func TestRequestImageVariantIDOwnsCompletePolicy(t *testing.T) {
	ref := harnessmodel.ImageAttachmentRef{
		AttachmentID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MediaType:    "image/png", Bytes: 10, Width: 2, Height: 2,
	}
	one, err := RequestImageVariantID(ref, ImageRequestPolicy{MaxPixels: 640_000, MaxBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	same, _ := RequestImageVariantID(ref, ImageRequestPolicy{MaxPixels: 640_000, MaxBytes: 1024 * 1024})
	differentPixels, _ := RequestImageVariantID(ref, ImageRequestPolicy{MaxPixels: 320_000, MaxBytes: 1024 * 1024})
	differentBytes, _ := RequestImageVariantID(ref, ImageRequestPolicy{MaxPixels: 640_000, MaxBytes: 512 * 1024})
	if one != same || one == differentPixels || one == differentBytes {
		t.Fatalf("ids = %q %q %q %q", one, same, differentPixels, differentBytes)
	}
}

func TestRequestImageRejectsInvalidPolicyAndReference(t *testing.T) {
	valid := harnessmodel.ImageAttachmentRef{
		AttachmentID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MediaType:    "image/png", Bytes: 10, Width: 2, Height: 2,
	}
	for _, policy := range []ImageRequestPolicy{{}, {MaxPixels: -1, MaxBytes: 1}, {MaxPixels: 1, MaxBytes: -1}} {
		if _, err := RequestImageVariantID(valid, policy); err == nil {
			t.Fatalf("policy %+v succeeded", policy)
		}
	}
	invalid := valid
	invalid.AttachmentID = "sha256:not-a-digest"
	if _, err := RequestImageVariantID(invalid, ImageRequestPolicy{MaxPixels: 1, MaxBytes: 1}); err == nil {
		t.Fatal("invalid attachment identity succeeded")
	}
}

func TestLocalAttachmentStoreCreatesOpaqueAndTransparentRequestVariants(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	opaque := image.NewGray(image.Rect(0, 0, 100, 50))
	transparent := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			opaque.SetGray(x, y, color.Gray{Y: uint8(x + y)})
			alpha := uint8(255)
			if x < 20 {
				alpha = 0
			}
			transparent.SetNRGBA(x, y, color.NRGBA{R: 10, G: 20, B: 200, A: alpha})
		}
	}
	opaqueRef := saveTestImage(t, store, opaque, "image/png")
	transparentRef := saveTestImage(t, store, transparent, "image/png")
	policy := ImageRequestPolicy{MaxPixels: 1_250, MaxBytes: 1024 * 1024}
	opaqueVersion, err := store.ReadRequestImage(context.Background(), opaqueRef, policy)
	if err != nil {
		t.Fatal(err)
	}
	transparentVersion, err := store.ReadRequestImage(context.Background(), transparentRef, policy)
	if err != nil {
		t.Fatal(err)
	}
	if opaqueVersion.Width != 50 || opaqueVersion.Height != 25 || opaqueVersion.MediaType != "image/jpeg" || opaqueVersion.HasAlpha {
		t.Fatalf("opaque = %+v", opaqueVersion)
	}
	if transparentVersion.Width != 50 || transparentVersion.Height != 25 || transparentVersion.MediaType != "image/png" || !transparentVersion.HasAlpha {
		t.Fatalf("transparent = %+v", transparentVersion)
	}
	if int64(len(opaqueVersion.Data)) != opaqueVersion.Bytes || int64(len(transparentVersion.Data)) != transparentVersion.Bytes {
		t.Fatal("variant byte metadata does not match encoded data")
	}
}

func TestLocalAttachmentStoreProjectsAnimatedGIFFirstFrame(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 20, 10), palette)
	second := image.NewPaletted(image.Rect(0, 0, 20, 10), palette)
	for index := range second.Pix {
		second.Pix[index] = 1
	}
	var source bytes.Buffer
	if err := gif.EncodeAll(&source, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: source.Bytes(), MediaType: "image/gif"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.ReadRequestImage(context.Background(), ref, ImageRequestPolicy{MaxPixels: 50, MaxBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if version.MediaType != "image/jpeg" || version.Width != 10 || version.Height != 5 {
		t.Fatalf("version = %+v", version)
	}
}

func TestLocalAttachmentStoreRegeneratesCorruptRequestCacheAndClonesResults(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: testPNG(t, 80, 40), MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root(), "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("image publication leaked staging links: %v, %v", entries, err)
	}
	policy := ImageRequestPolicy{MaxPixels: 800, MaxBytes: 1024 * 1024}
	variantID, err := RequestImageVariantID(ref, policy)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.requestImagePath(variantID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadRequestImage(context.Background(), ref, policy)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate corruption deliberately, including Windows read-only attributes.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o400); err != nil {
		t.Fatal(err)
	}
	second, err := store.ReadRequestImage(context.Background(), ref, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Fatal("regenerated request image differs")
	}
	second.Data[0] ^= 0xff
	third, err := store.ReadRequestImage(context.Background(), ref, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Data, third.Data) {
		t.Fatal("caller mutated cached request bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0o400)
	if runtime.GOOS == "windows" {
		wantMode = 0o444 // Windows exposes only the shared read-only attribute.
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("cache mode=%v", info.Mode().Perm())
	}
}

func TestLocalAttachmentStoreSharesConcurrentRequestImageWork(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: testPNG(t, 400, 300), MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	policy := ImageRequestPolicy{MaxPixels: 30_000, MaxBytes: 1024 * 1024}
	const workers = 24
	results := make([]RequestImageAttachment, workers)
	errs := make([]error, workers)
	var wait sync.WaitGroup
	for index := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index], errs[index] = store.ReadRequestImage(context.Background(), ref, policy)
		}()
	}
	wait.Wait()
	for index := range workers {
		if errs[index] != nil || results[index].VariantID != results[0].VariantID || !bytes.Equal(results[index].Data, results[0].Data) {
			t.Fatalf("result[%d]=%+v err=%v", index, results[index], errs[index])
		}
	}
}

func saveTestImage(t *testing.T, store *LocalAttachmentStore, source image.Image, mediaType string) harnessmodel.ImageAttachmentRef {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: encoded.Bytes(), MediaType: mediaType})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRequestImageExactTargetHasDistinctIdentityAndEncoding(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref := saveTestImage(t, store, image.NewGray(image.Rect(0, 0, 100, 100)), "image/png")
	first := ImageRequestPolicy{MaxPixels: 1250, MaxBytes: 1024 * 1024, Width: 50, Height: 25}
	second := ImageRequestPolicy{MaxPixels: 1250, MaxBytes: 1024 * 1024, Width: 25, Height: 50}
	one, err := store.ReadRequestImage(context.Background(), ref, first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := store.ReadRequestImage(context.Background(), ref, second)
	if err != nil {
		t.Fatal(err)
	}
	if one.VariantID == two.VariantID || one.Width != 50 || one.Height != 25 || two.Width != 25 || two.Height != 50 {
		t.Fatalf("exact targets collided: one=%+v two=%+v", one, two)
	}
	for _, policy := range []ImageRequestPolicy{{MaxPixels: 1, MaxBytes: 1, Width: 1}, {MaxPixels: 1, MaxBytes: 1, Width: 2, Height: 2}, {MaxPixels: 100000, MaxBytes: 1, Width: 101, Height: 101}} {
		if _, err := store.ReadRequestImage(context.Background(), ref, policy); err == nil {
			t.Fatalf("invalid target accepted: %+v", policy)
		}
	}
}
