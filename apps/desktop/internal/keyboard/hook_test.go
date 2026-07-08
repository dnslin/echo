package keyboard

import "testing"

func TestTransitionTrackerEmitsTargetKeyDownAndUp(t *testing.T) {
	tracker := newTransitionTracker("V")

	down, ok := tracker.apply("v", true)
	if !ok {
		t.Fatal("expected target key down event")
	}
	if down.Key != "V" || !down.Pressed || down.Source != SourceNative {
		t.Fatalf("unexpected down event: %#v", down)
	}

	up, ok := tracker.apply("V", false)
	if !ok {
		t.Fatal("expected target key up event")
	}
	if up.Key != "V" || up.Pressed || up.Source != SourceNative {
		t.Fatalf("unexpected up event: %#v", up)
	}
}

func TestTransitionTrackerIgnoresRepeatedDown(t *testing.T) {
	tracker := newTransitionTracker("V")

	if _, ok := tracker.apply("V", true); !ok {
		t.Fatal("expected first down event")
	}
	if event, ok := tracker.apply("V", true); ok {
		t.Fatalf("expected repeated down to be ignored, got %#v", event)
	}
	if _, ok := tracker.apply("V", false); !ok {
		t.Fatal("expected release after first down")
	}
}

func TestTransitionTrackerIgnoresNonTargetKey(t *testing.T) {
	tracker := newTransitionTracker("V")

	if event, ok := tracker.apply("B", true); ok {
		t.Fatalf("expected non-target key to be ignored, got %#v", event)
	}
}
