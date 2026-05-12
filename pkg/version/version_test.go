package version

import "testing"

func TestVersion_IsV140(t *testing.T) {
	if Version != "v1.4.0" {
		t.Errorf("Version = %q, want v1.4.0", Version)
	}
}
