package harness

import harnessmodel "github.com/phodal/pero/harness/go/model"

func cloneContentBlocks(content []ContentBlock) []ContentBlock {
	return harnessmodel.CloneContentBlocks(content)
}

func cloneContentBlock(block ContentBlock) ContentBlock {
	return harnessmodel.CloneContentBlock(block)
}

func cloneImageAttachmentRef(ref ImageAttachmentRef) ImageAttachmentRef {
	return harnessmodel.CloneImageAttachmentRef(ref)
}
