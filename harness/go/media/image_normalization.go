package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"
)

type imageNormalizationPolicy struct {
	MaxPixels    int64
	MaxDimension int
	MaxBytes     int64
}

type normalizedImage struct {
	Data      []byte
	MediaType string
	Width     int
	Height    int
}

func normalizeImage(
	data []byte,
	mediaType string,
	orientation int,
	policy imageNormalizationPolicy,
) (normalizedImage, error) {
	if policy.MaxPixels <= 0 || policy.MaxDimension <= 0 || policy.MaxBytes <= 0 {
		return normalizedImage{}, newAttachmentError("Image normalization limits must be positive.", "ATTACHMENT_WRITE_FAILED", nil)
	}
	if carriesICCProfile(data, mediaType) {
		return normalizedImage{}, newAttachmentError(
			"The image has an embedded color profile that this Go deployment cannot safely convert to sRGB.",
			"ATTACHMENT_WRITE_FAILED", nil,
		)
	}
	decoded, err := decodeStoredImage(data, mediaType)
	if err != nil {
		return normalizedImage{}, newAttachmentError("Unsupported or malformed image data.", "INVALID_IMAGE", err)
	}
	if canPassThroughImage(data, mediaType, decoded, orientation, policy) {
		return normalizedImage{Data: append([]byte(nil), data...), MediaType: mediaType, Width: decoded.Bounds().Dx(), Height: decoded.Bounds().Dy()}, nil
	}
	oriented := orientImage(decoded, orientation)
	width, height, err := normalizedImageDimensions(oriented.Bounds().Dx(), oriented.Bounds().Dy(), policy)
	if err != nil {
		return normalizedImage{}, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	if oriented.Bounds().Dx() == width && oriented.Bounds().Dy() == height {
		drawImage(canvas, oriented)
	} else {
		drawCatmullRom(canvas, oriented)
	}
	hasAlpha := inspectImageFacts(data, mediaType, decoded).hasAlpha
	var encoded []byte
	outputType := "image/png"
	if hasAlpha {
		var output bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&output, alphaPlaneImage{canvas}); err != nil {
			return normalizedImage{}, newAttachmentError("Unable to normalize the image.", "ATTACHMENT_WRITE_FAILED", err)
		}
		encoded = output.Bytes()
	} else {
		var smallest []byte
		for _, quality := range requestImageJPEGQualities {
			var output bytes.Buffer
			if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: quality}); err != nil {
				return normalizedImage{}, newAttachmentError("Unable to normalize the image.", "ATTACHMENT_WRITE_FAILED", err)
			}
			candidate := output.Bytes()
			if smallest == nil || len(candidate) < len(smallest) {
				smallest = append([]byte(nil), candidate...)
			}
			if int64(len(candidate)) <= policy.MaxBytes {
				encoded = append([]byte(nil), candidate...)
				break
			}
		}
		if encoded == nil {
			encoded = smallest
		}
		outputType = "image/jpeg"
	}
	verifiedWidth, verifiedHeight, verifiedType, err := probeImageMetadata(encoded)
	if err != nil || verifiedWidth != width || verifiedHeight != height || verifiedType != outputType {
		return normalizedImage{}, newAttachmentError("Normalized image failed verification.", "ATTACHMENT_WRITE_FAILED", err)
	}
	verified, err := decodeStoredImage(encoded, outputType)
	if err != nil || inspectImageFacts(encoded, outputType, verified).hasAlpha != hasAlpha {
		return normalizedImage{}, newAttachmentError("Normalized image alpha facts failed verification.", "ATTACHMENT_WRITE_FAILED", err)
	}
	return normalizedImage{Data: append([]byte(nil), encoded...), MediaType: outputType, Width: width, Height: height}, nil
}

func normalizedImageDimensions(width, height int, policy imageNormalizationPolicy) (int, int, error) {
	width, height, err := RequestImageDimensions(width, height, policy.MaxPixels)
	if err != nil {
		return 0, 0, err
	}
	longEdge := max(width, height)
	if longEdge <= policy.MaxDimension {
		return width, height, nil
	}
	scale := float64(policy.MaxDimension) / float64(longEdge)
	return max(1, int(math.Floor(float64(width)*scale))), max(1, int(math.Floor(float64(height)*scale))), nil
}

func orientImage(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	outputWidth, outputHeight := width, height
	if orientation >= 5 {
		outputWidth, outputHeight = height, width
	}
	return orientedImage{
		source: source, sourceBounds: bounds, orientation: orientation,
		bounds: image.Rect(0, 0, outputWidth, outputHeight),
	}
}

type orientedImage struct {
	source       image.Image
	sourceBounds image.Rectangle
	bounds       image.Rectangle
	orientation  int
}

func (i orientedImage) ColorModel() color.Model { return i.source.ColorModel() }
func (i orientedImage) Bounds() image.Rectangle { return i.bounds }
func (i orientedImage) At(x, y int) color.Color {
	if !image.Pt(x, y).In(i.bounds) {
		return color.NRGBA{}
	}
	width, height := i.sourceBounds.Dx(), i.sourceBounds.Dy()
	var sourceX, sourceY int
	switch i.orientation {
	case 2:
		sourceX, sourceY = width-1-x, y
	case 3:
		sourceX, sourceY = width-1-x, height-1-y
	case 4:
		sourceX, sourceY = x, height-1-y
	case 5:
		sourceX, sourceY = y, x
	case 6:
		sourceX, sourceY = y, height-1-x
	case 7:
		sourceX, sourceY = width-1-y, height-1-x
	case 8:
		sourceX, sourceY = width-1-y, x
	}
	return i.source.At(i.sourceBounds.Min.X+sourceX, i.sourceBounds.Min.Y+sourceY)
}

func drawImage(destination *image.NRGBA, source image.Image) {
	stddraw.Draw(destination, destination.Bounds(), source, source.Bounds().Min, stddraw.Src)
}

func drawCatmullRom(destination *image.NRGBA, source image.Image) {
	xdraw.CatmullRom.Scale(destination, destination.Bounds(), source, source.Bounds(), xdraw.Over, nil)
}

func imageOrientation(data []byte, mediaType string) (int, error) {
	payload, found, err := exifPayload(data, mediaType)
	if err != nil {
		return 0, newAttachmentError("Image metadata is malformed.", "INVALID_IMAGE", err)
	}
	if !found {
		return 1, nil
	}
	orientation, err := tiffOrientation(payload)
	if err != nil {
		return 0, newAttachmentError("Image EXIF orientation is malformed.", "INVALID_IMAGE", err)
	}
	return orientation, nil
}

func exifPayload(data []byte, mediaType string) ([]byte, bool, error) {
	switch mediaType {
	case "image/jpeg":
		var payload []byte
		err := visitJPEGSegments(data, func(marker byte, segment []byte) bool {
			if marker == 0xe1 && len(segment) >= 6 && bytes.Equal(segment[:6], []byte("Exif\x00\x00")) {
				payload = segment[6:]
				return false
			}
			return true
		})
		return payload, payload != nil, err
	case "image/png":
		for offset := 8; offset+12 <= len(data); {
			length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
			if length < 0 || offset+12+length > len(data) {
				return nil, false, errors.New("invalid PNG chunk length")
			}
			if string(data[offset+4:offset+8]) == "eXIf" {
				return data[offset+8 : offset+8+length], true, nil
			}
			offset += 12 + length
		}
	case "image/webp":
		for offset := 12; offset+8 <= len(data); {
			length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			end := offset + 8 + length
			if length < 0 || end > len(data) {
				return nil, false, errors.New("invalid WebP chunk length")
			}
			if string(data[offset:offset+4]) == "EXIF" {
				payload := data[offset+8 : end]
				if len(payload) >= 6 && bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
					payload = payload[6:]
				}
				return payload, true, nil
			}
			offset = end + length%2
		}
	}
	return nil, false, nil
}

func tiffOrientation(data []byte) (int, error) {
	if len(data) < 8 {
		return 0, errors.New("short TIFF header")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, errors.New("invalid TIFF byte order")
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, errors.New("invalid TIFF magic")
	}
	offset := int(order.Uint32(data[4:8]))
	if offset < 0 || offset+2 > len(data) {
		return 0, errors.New("invalid TIFF IFD offset")
	}
	count := int(order.Uint16(data[offset : offset+2]))
	entries := offset + 2
	if count > (len(data)-entries)/12 {
		return 0, errors.New("invalid TIFF IFD length")
	}
	for index := 0; index < count; index++ {
		entry := data[entries+index*12 : entries+(index+1)*12]
		if order.Uint16(entry[:2]) != 0x0112 {
			continue
		}
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, errors.New("invalid orientation field")
		}
		value := int(order.Uint16(entry[8:10]))
		if value < 1 || value > 8 {
			return 0, errors.New("invalid orientation value")
		}
		return value, nil
	}
	return 1, nil
}

func visitJPEGSegments(data []byte, visit func(byte, []byte) bool) error {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return errors.New("invalid JPEG header")
	}
	for offset := 2; offset < len(data); {
		if data[offset] != 0xff {
			return errors.New("invalid JPEG marker")
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			return errors.New("short JPEG marker")
		}
		marker := data[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			return nil
		}
		if marker == 0x00 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(data) {
			return errors.New("short JPEG segment")
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || offset+length > len(data) {
			return errors.New("invalid JPEG segment length")
		}
		if !visit(marker, data[offset+2:offset+length]) {
			return nil
		}
		offset += length
	}
	return nil
}

func carriesICCProfile(data []byte, mediaType string) bool {
	switch mediaType {
	case "image/jpeg":
		found := false
		_ = visitJPEGSegments(data, func(marker byte, segment []byte) bool {
			found = marker == 0xe2 && len(segment) >= len("ICC_PROFILE\x00") &&
				bytes.Equal(segment[:len("ICC_PROFILE\x00")], []byte("ICC_PROFILE\x00"))
			return !found
		})
		return found
	case "image/png":
		for offset := 8; offset+12 <= len(data); {
			length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
			if length < 0 || offset+12+length > len(data) {
				return false
			}
			if string(data[offset+4:offset+8]) == "iCCP" {
				return true
			}
			offset += 12 + length
		}
	case "image/webp":
		for offset := 12; offset+8 <= len(data); {
			length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			end := offset + 8 + length
			if length < 0 || end > len(data) {
				return false
			}
			if string(data[offset:offset+4]) == "ICCP" {
				return true
			}
			offset = end + length%2
		}
	}
	return false
}
