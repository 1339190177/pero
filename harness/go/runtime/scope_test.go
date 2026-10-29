package runtime

import "testing"

func TestScopeContainsSelfAndDescendants(t *testing.T) {
	runtime := NewRuntime(Options{})
	root := runtime.Root()
	child := root.Child()
	grandchild := child.Child()
	other := NewRuntime(Options{}).Root()

	for name, test := range map[string]struct {
		scope, candidate *Scope
		want             bool
	}{
		"self":               {root, root, true},
		"child":              {root, child, true},
		"grandchild":         {root, grandchild, true},
		"parent-not-visible": {child, root, false},
		"sibling":            {child, root.Child(), false},
		"other-runtime":      {root, other, false},
		"nil-scope":          {nil, child, false},
		"nil-candidate":      {root, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := test.scope.Contains(test.candidate); got != test.want {
				t.Fatalf("Contains() = %v, want %v", got, test.want)
			}
		})
	}
}
