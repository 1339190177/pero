package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestOrientedImageAppliesEXIFTransformsWithoutChangingSource(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	colours := []color.NRGBA{
		{R: 1, A: 255}, {R: 2, A: 255},
		{R: 3, A: 255}, {R: 4, A: 255},
		{R: 5, A: 255}, {R: 6, A: 255},
	}
	for index, colour := range colours {
		source.SetNRGBA(index%2, index/2, colour)
	}
	rotated := orientImage(source, 6)
	if rotated.Bounds().Dx() != 3 || rotated.Bounds().Dy() != 2 {
		t.Fatalf("bounds = %v", rotated.Bounds())
	}
	want := [][]uint32{{5, 3, 1}, {6, 4, 2}}
	for y := range want {
		for x := range want[y] {
			red, _, _, _ := rotated.At(x, y).RGBA()
			if red>>8 != want[y][x] {
				t.Fatalf("pixel(%d,%d)=%d", x, y, red>>8)
			}
		}
	}
	if red, _, _, _ := source.At(0, 0).RGBA(); red>>8 != 1 {
		t.Fatal("orientation mutated source")
	}
}

func TestLocalAttachmentStoreAppliesAndStripsEXIFOrientation(t *testing.T) {
	canvas := image.NewGray(image.Rect(0, 0, 2, 3))
	canvas.SetGray(0, 0, color.Gray{Y: 10})
	canvas.SetGray(1, 2, color.Gray{Y: 240})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	withEXIF := insertJPEGEXIF(t, encoded.Bytes(), 6)
	if orientation, err := imageOrientation(withEXIF, "image/jpeg"); err != nil || orientation != 6 {
		t.Fatalf("orientation=%d err=%v", orientation, err)
	}
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: withEXIF, MediaType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Width != 3 || ref.Height != 2 || ref.MediaType != "image/jpeg" {
		t.Fatalf("ref = %+v", ref)
	}
	stored, err := store.ReadImage(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if orientation, err := imageOrientation(stored.Data, stored.Ref.MediaType); err != nil || orientation != 1 {
		t.Fatalf("normalized orientation=%d err=%v", orientation, err)
	}
}

func TestLocalAttachmentStoreNormalizesToConfiguredPixelBudget(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{
		Home: t.TempDir(), NormalizedImageMaxPixels: 1_250, NormalizedImageMaxDimension: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.SaveImage(context.Background(), SaveImageAttachment{Data: testPNG(t, 100, 50), MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Width != 50 || ref.Height != 25 || ref.MediaType != "image/jpeg" || ref.OriginalDimensions == nil || ref.OriginalDimensions.Width != 100 || ref.OriginalDimensions.Height != 50 {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestLocalAttachmentStoreFailsClosedForUnconvertibleICCProfile(t *testing.T) {
	canvas := image.NewGray(image.Rect(0, 0, 2, 2))
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, nil); err != nil {
		t.Fatal(err)
	}
	withICC := insertJPEGSegment(encoded.Bytes(), 0xe2, append([]byte("ICC_PROFILE\x00"), 1, 1, 0))
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveImage(context.Background(), SaveImageAttachment{Data: withICC, MediaType: "image/jpeg"})
	var attachment *AttachmentError
	if !errors.As(err, &attachment) || attachment.Code != "ATTACHMENT_WRITE_FAILED" {
		t.Fatalf("error = %v", err)
	}
}

func TestLocalAttachmentStoreRejectsInvalidNormalizationLimits(t *testing.T) {
	for _, config := range []LocalAttachmentStoreConfig{
		{NormalizedImageMaxPixels: -1}, {NormalizedImageMaxDimension: -1}, {NormalizedImageMaxBytes: -1},
		{ImageCompressionConcurrency: -1}, {ImageCompressionConcurrency: MaxImageCompressionConcurrency + 1},
	} {
		config.Home = t.TempDir()
		if _, err := OpenLocalAttachmentStore(config); err == nil {
			t.Fatalf("config %+v succeeded", config)
		}
	}
}

func insertJPEGEXIF(t *testing.T, source []byte, orientation uint16) []byte {
	t.Helper()
	tiff := make([]byte, 8+2+12+4)
	copy(tiff[:2], "II")
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	entry := tiff[10:22]
	binary.LittleEndian.PutUint16(entry[:2], 0x0112)
	binary.LittleEndian.PutUint16(entry[2:4], 3)
	binary.LittleEndian.PutUint32(entry[4:8], 1)
	binary.LittleEndian.PutUint16(entry[8:10], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	return insertJPEGSegment(source, 0xe1, payload)
}

func insertJPEGSegment(source []byte, marker byte, payload []byte) []byte {
	segment := make([]byte, 4+len(payload))
	segment[0], segment[1] = 0xff, marker
	binary.BigEndian.PutUint16(segment[2:4], uint16(len(payload)+2))
	copy(segment[4:], payload)
	result := append([]byte(nil), source[:2]...)
	result = append(result, segment...)
	result = append(result, source[2:]...)
	return result
}
