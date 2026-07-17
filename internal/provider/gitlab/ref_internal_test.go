package gitlab

import (
	"testing"

	gl "github.com/xanzy/go-gitlab"
)

// projectFromRef must strip both the issue sigil '#' and the merge-request
// sigil '!' so cross-project listings recover the bare "namespace/project".
func TestProjectFromRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		full string
		want string
	}{
		{"issue ref", "group/proj#7", "group/proj"},
		{"mr ref", "group/proj!3", "group/proj"},
		{"nested group mr", "a/b/c!42", "a/b/c"},
		{"no sigil", "group/proj", "group/proj"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectFromRef(&gl.IssueReferences{Full: tc.full})
			if got != tc.want {
				t.Errorf("projectFromRef(%q) = %q; want %q", tc.full, got, tc.want)
			}
		})
	}
	if got := projectFromRef(nil); got != "" {
		t.Errorf("projectFromRef(nil) = %q; want empty", got)
	}
}
