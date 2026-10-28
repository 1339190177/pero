package model

import "encoding/json"

func CloneMessage(message Message) Message {
	message.Content = CloneContentBlocks(message.Content)
	message.Source = append(json.RawMessage(nil), message.Source...)
	return message
}

func CloneMessages(messages []Message) []Message {
	if messages == nil {
		return nil
	}
	result := make([]Message, len(messages))
	for index := range messages {
		result[index] = CloneMessage(messages[index])
	}
	return result
}

// CloneContentBlocks returns a deep copy of a content tree, including wire
// payload bytes and attachment metadata.
func CloneContentBlocks(content []ContentBlock) []ContentBlock {
	if content == nil {
		return nil
	}
	result := make([]ContentBlock, len(content))
	for index, block := range content {
		result[index] = CloneContentBlock(block)
	}
	return result
}

func CloneContentBlock(block ContentBlock) ContentBlock {
	attachmentExtra := block.ownedAttachmentExtensions()
	block.attachmentExtensions = nil
	if block.Extensions != nil {
		extensions := make(map[string]json.RawMessage, len(block.Extensions))
		for key, value := range block.Extensions {
			extensions[key] = append(json.RawMessage(nil), value...)
		}
		block.Extensions = extensions
	}
	block.Data = append([]byte(nil), block.Data...)
	if block.Attachment != nil {
		attachment := CloneImageAttachmentRef(*block.Attachment)
		block.Attachment = &attachment
	}
	if block.FileAttachment != nil {
		ref := *block.FileAttachment
		block.FileAttachment = &ref
	}
	if attachmentExtra != nil {
		block.attachmentExtensions = bindAttachmentExtensions(block,
			append(json.RawMessage(nil), attachmentExtra.fields...),
			append(json.RawMessage(nil), attachmentExtra.dimensions...))
	}
	block.Arguments = append(json.RawMessage(nil), block.Arguments...)
	block.Meta = append(json.RawMessage(nil), block.Meta...)
	block.Content = CloneContentBlocks(block.Content)
	return block
}

func CloneImageAttachmentRef(ref ImageAttachmentRef) ImageAttachmentRef {
	if ref.OriginalDimensions != nil {
		dimensions := *ref.OriginalDimensions
		ref.OriginalDimensions = &dimensions
	}
	return ref
}
