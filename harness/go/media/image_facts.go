package media

import (
	"encoding/binary"
	"image"
)

// imageFacts derives container facts after a complete successful decode. Go's
// PNG decoder uses *image.RGBA for RGB PNGs too, so the decoded Go type does not
// establish whether the source declares an alpha plane.
type imageFacts struct {
	hasAlpha, metadata, animated bool
	depth, space                 string
}

func inspectImageFacts(data []byte, mediaType string, decoded image.Image) imageFacts {
	facts := imageFacts{hasAlpha: imageHasAlphaChannel(decoded), depth: "uchar", space: "srgb"}
	switch decoded.(type) {
	case *image.Gray, *image.Gray16:
		facts.space = "b-w"
	case *image.CMYK:
		facts.space = "cmyk"
	}
	switch mediaType {
	case "image/png":
		if len(data) < 33 {
			facts.metadata = true
			return facts
		}
		if data[24] == 16 {
			facts.depth = "ushort"
		}
		if data[25] == 0 || data[25] == 4 {
			facts.space = "b-w"
		}
		facts.hasAlpha = data[25] == 4 || data[25] == 6
		for offset := 8; offset+12 <= len(data); {
			length := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			if length > uint64(len(data)-offset-12) {
				facts.metadata = true
				break
			}
			switch string(data[offset+4 : offset+8]) {
			case "tRNS":
				facts.hasAlpha = true
			case "acTL":
				facts.animated = true
			case "eXIf", "iCCP", "tEXt", "zTXt", "iTXt":
				facts.metadata = true
			}
			offset += int(length) + 12
		}
	case "image/jpeg":
		facts.hasAlpha = false
		_ = visitJPEGSegments(data, func(marker byte, _ []byte) bool {
			if marker == 0xe1 || marker == 0xe2 || marker == 0xed || marker == 0xfe {
				facts.metadata = true
			}
			return true
		})
	case "image/webp":
		facts.hasAlpha = false
		for offset := 12; offset+8 <= len(data); {
			length := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			if length > uint64(len(data)-offset-8) {
				facts.metadata = true
				break
			}
			payload := data[offset+8 : offset+8+int(length)]
			switch string(data[offset : offset+4]) {
			case "VP8X":
				if len(payload) > 0 {
					facts.hasAlpha = payload[0]&0x10 != 0
					facts.animated = payload[0]&0x02 != 0
				}
			case "VP8L":
				if len(payload) >= 5 {
					facts.hasAlpha = facts.hasAlpha || payload[4]&0x10 != 0
				}
			case "ALPH":
				facts.hasAlpha = true
			case "EXIF", "XMP ", "ICCP":
				facts.metadata = true
			}
			offset += 8 + int(length) + int(length%2)
		}
	case "image/gif":
		// Every GIF must normalize, even when its first frame is opaque.
		facts.animated = true
	}
	return facts
}

// Native PNG fallback preserves a declared alpha plane even when every sample
// is opaque. Otherwise image/png silently drops it during normalization.
type alphaPlaneImage struct{ image.Image }

func (alphaPlaneImage) Opaque() bool { return false }

func canPassThroughImage(data []byte, mediaType string, decoded image.Image, orientation int, policy imageNormalizationPolicy) bool {
	facts := inspectImageFacts(data, mediaType, decoded)
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	return mediaType != "image/gif" && orientation == 1 && !facts.metadata && !facts.animated &&
		facts.depth == "uchar" && facts.space == "srgb" && int64(len(data)) <= policy.MaxBytes &&
		int64(width) <= policy.MaxPixels/int64(height) && max(width, height) <= policy.MaxDimension
}
