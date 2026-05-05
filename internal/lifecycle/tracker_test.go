package lifecycle

import "testing"

func TestTracker_MarkShuttingDown(t *testing.T) {
	tracker := NewTracker()
	if tracker == nil {
		t.Fatal("expected non-nil Tracker")
	}
	// MarkShuttingDown should not panic
	tracker.MarkShuttingDown()
}

func TestTracker_Wait(t *testing.T) {
	tracker := NewTracker()
	// Wait with no in-flight requests should return immediately
	tracker.MarkShuttingDown()
	tracker.Wait()
}
