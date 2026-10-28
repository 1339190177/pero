package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ImageAttachmentIdentity matches DSH imageIdentity: a bare attachment id, or a
// JSON string of the display name plus the id.
func ImageAttachmentIdentity(ref ImageAttachmentRef) string {
	if ref.Name == "" {
		return ref.AttachmentID
	}
	return imageJSONQuote(ref.Name) + " (" + ref.AttachmentID + ")"
}

// OffloadedImageText is the stable request-limit placeholder. An empty
// readonlyPath keeps the no-path recovery sentence; a path names the normalized
// read-only object and the extension a caller must copy before editing.
func OffloadedImageText(block ContentBlock, readonlyPath string) string {
	if block.Attachment == nil {
		return "[image omitted to fit request image limits; no durable attachment identity is available. Ask the user to attach it again if needed.]"
	}
	identity := "image omitted to fit request image limits; " + ImageAttachmentIdentity(*block.Attachment) + "."
	if readonlyPath == "" {
		return "[" + identity + " No local normalized image path is available; ask the user to attach it again if needed.]"
	}
	return "[" + identity + normalizedImageAccessText(*block.Attachment, readonlyPath) + "]"
}

func normalizedImageAccessText(ref ImageAttachmentRef, readonlyPath string) string {
	quoted := imageJSONQuote(readonlyPath)
	return " Normalized copy (read-only; may be resized or re-encoded): " + quoted +
		fmt.Sprintf(" (%dx%dpx, %s).", ref.Width, ref.Height, ref.MediaType) +
		" Source dimensions, format, and byte size may differ." +
		" Copy to a writable path ending in " + imageFileExtension(ref.MediaType) + " before editing."
}

func imageFileExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

// RequestImageHandleText uses the same normalized-object access text as an
// offload placeholder while identifying the actual request preview geometry.
func RequestImageHandleText(ref ImageAttachmentRef, width, height int, readonlyPath string) string {
	preview := fmt.Sprintf("Image %s; request preview %dx%dpx.", ImageAttachmentIdentity(ref), width, height)
	if readonlyPath == "" {
		return preview + " It may be resized or re-encoded; source dimensions, format, and byte size may differ."
	}
	return preview + normalizedImageAccessText(ref, readonlyPath)
}

func imageJSONQuote(value string) string {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(output.String(), "\n")
}
