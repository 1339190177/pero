package media

import harnessmodel "github.com/phodal/pero/harness/go/model"

// ProjectFilesToText leaves durable inputs untouched and gives every provider
// the same handle. A custom store without a host path reports unavailable
// access; no filesystem path supplied in a request becomes authority.
func ProjectFilesToText(messages []harnessmodel.Message, store AttachmentStore) ([]harnessmodel.Message, error) {
	hasFiles := false
	for _, message := range messages {
		if contentHasFile(message.Content) {
			hasFiles = true
			break
		}
	}
	if !hasFiles {
		return messages, nil
	}
	var err error
	result := harnessmodel.CloneMessages(messages)
	var visit func([]harnessmodel.ContentBlock)
	visit = func(blocks []harnessmodel.ContentBlock) {
		for i := range blocks {
			block := &blocks[i]
			if block.Type == harnessmodel.BlockFile {
				if block.FileAttachment == nil {
					err = newAttachmentError("File attachment reference is invalid.", "INVALID_ATTACHMENT_REF", nil)
					return
				}
				ref := *block.FileAttachment
				if err = ValidateFileAttachmentRef(ref); err != nil {
					return
				}
				path := ""
				if host, ok := store.(FileHostPathStore); ok {
					path, err = host.FileHostPath(ref)
					if err != nil {
						return
					}
				}
				*block = harnessmodel.ContentBlock{Type: harnessmodel.BlockText, Text: harnessmodel.FileHandleText(ref, path)}
			} else {
				visit(block.Content)
				if err != nil {
					return
				}
			}
		}
	}
	for i := range result {
		visit(result[i].Content)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func contentHasFile(blocks []harnessmodel.ContentBlock) bool {
	for _, block := range blocks {
		if block.Type == harnessmodel.BlockFile || contentHasFile(block.Content) {
			return true
		}
	}
	return false
}
