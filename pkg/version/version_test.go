package version

import "testing"

func TestVersion_IsV141(t *testing.T) {
	if Version != "v1.4.1" {
		t.Errorf("Version = %q, want v1.4.1", Version)
	}
}
