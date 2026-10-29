package media

import harnessmodel "github.com/phodal/pero/harness/go/model"

// ImageAttachmentAccess is the execution-world location of one normalized image.
type ImageAttachmentAccess struct {
	ReadonlyPath string
}

// ImageHostPathStore exposes the provider-local object path for one durable image.
type ImageHostPathStore interface {
	ImageHostPath(harnessmodel.ImageAttachmentRef) (string, error)
}

// ResolveImageAttachmentAccess maps a durable image into the current tool
// execution world. A store that has no host path, or a mapper that declines
// the path, returns no access. mapHostPath nil keeps the host path. An invalid
// reference returns the attachment error.
func ResolveImageAttachmentAccess(store AttachmentStore, ref harnessmodel.ImageAttachmentRef, mapHostPath func(string) (string, bool)) (*ImageAttachmentAccess, error) {
	provider, ok := store.(ImageHostPathStore)
	if store == nil || !ok {
		return nil, nil
	}
	hostPath, err := provider.ImageHostPath(ref)
	if err != nil {
		return nil, err
	}
	if hostPath == "" {
		return nil, nil
	}
	readonly := hostPath
	if mapHostPath != nil {
		mapped, ok := mapHostPath(hostPath)
		if !ok || mapped == "" {
			return nil, nil
		}
		readonly = mapped
	}
	return &ImageAttachmentAccess{ReadonlyPath: readonly}, nil
}
