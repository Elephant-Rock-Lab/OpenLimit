package version

import "testing"

func TestVersion_IsV142(t *testing.T) {
	if Version != "v1.4.2" {
		t.Errorf("Version = %q, want v1.4.2", Version)
	}
}
