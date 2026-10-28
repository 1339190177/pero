package tool

import (
	"bytes"
	"encoding/json"
	harnessmodel "github.com/phodal/pero/harness/go/model"
	"reflect"
)

type presentationBaseline struct {
	binding any
	value   json.RawMessage
	content []harnessmodel.ContentBlock
	meta    json.RawMessage
}

// CapturePresentation is a framework primitive. The opaque binding is supplied
// by the execution owner after canonical validation, never by a guest descriptor.
func CapturePresentation(result Result, binding any) Result {
	result.presentation = &presentationBaseline{binding: binding, value: append(json.RawMessage(nil), result.Value...), content: harnessmodel.CloneContentBlocks(result.Content), meta: append(json.RawMessage(nil), result.Meta...)}
	return result
}

// InheritPresentation prevents a policy's reconstructed Result from changing
// the provenance of the accepted pre-policy presentation.
func InheritPresentation(result, previous Result) Result {
	result.presentation = previous.presentation
	return result
}
func ClearPresentation(result Result) Result { result.presentation = nil; return result }

// InspectPresentation reveals only detached comparison results and an opaque
// execution-owner binding. Callers must authenticate that binding themselves.
func InspectPresentation(result Result) (binding any, contentChanged, metaChanged, valid bool) {
	baseline := result.presentation
	if baseline == nil || result.IsError || !bytes.Equal(result.Value, baseline.value) {
		return nil, false, false, false
	}
	return baseline.binding, !reflect.DeepEqual(result.Content, baseline.content), !bytes.Equal(result.Meta, baseline.meta), true
}
