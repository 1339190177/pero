# Media value/storage seam

This package preserves the attachment interfaces referenced by the core harness.
The initial explicit chat provider advertises text only and rejects image/file
content instead of uploading or silently projecting it. No codec is started by
importing this package. The optional official codec bridge requires an explicitly
configured deployment anchor; its external Node dependency is not bundled.

Pure fixture tests cover local attachment storage, cleanup, image normalization,
projection and request-image constraints. The external DSH source-oracle test
suite is deliberately not included or claimed; no official source checkout or
real model is used. SOURCE.json records provenance and
DEEPSEEK_IMAGE_LICENSE.txt preserves the upstream notice.
