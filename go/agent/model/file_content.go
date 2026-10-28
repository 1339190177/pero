// Contract algorithms ported from DeepSeek DSH (MIT); see
// DEEPSEEK_FILES_LICENSE.txt for the retained notice.
package model

import (
	"fmt"
	"strings"
)

// FileHandleText follows the official deterministic file representation. The
// saved path is supplied by trusted storage/execution mapping, never the user.
func FileHandleText(ref FileAttachmentRef, readonlyPath string) string {
	quote := imageJSONQuote
	digest := strings.TrimPrefix(ref.AttachmentID, "sha256:")
	if len(digest) > 8 {
		digest = digest[:8]
	}
	identity := fmt.Sprintf("File %s (%d bytes, sha256:%s)", quote(ref.Name), ref.Bytes, digest)
	if readonlyPath == "" {
		return "[" + identity + " was uploaded, but the current execution environment cannot access a readable path. Report that limitation if its contents are needed; do not claim to have read it.]"
	}
	return "[" + identity + ": verbatim read-only copy saved at " + quote(readonlyPath) + ". Read that path with your file tools when its contents are needed; copy it to a writable location before modifying it. When delegating file work, include this saved path in the delegation prompt; only subagents sharing this execution environment can read it.]"
}
