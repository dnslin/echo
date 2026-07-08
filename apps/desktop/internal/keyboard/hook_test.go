package keyboard

import "testing"

func TestTransitionTrackerEmitsTargetKeyDownAndUp(t *testing.T) {
	tracker := newTransitionTracker("V")

	down, ok := tracker.apply("v", true)
	if !ok {
		t.Fatal("expected target key down event")
	}
	if down.Key != "V" || !down.Pressed || down.Source != SourceNative || down.Sequence != 1 {
		t.Fatalf("unexpected down event: %#v", down)
	}

	up, ok := tracker.apply("V", false)
	if !ok {
		t.Fatal("expected target key up event")
	}
	if up.Key != "V" || up.Pressed || up.Source != SourceNative || up.Sequence != 2 {
		t.Fatalf("unexpected up event: %#v", up)
	}
}

func TestTransitionTrackerAssignsMonotonicSequencesOnlyToTransitions(t *testing.T) {
	tracker := newTransitionTracker("V")

	down, ok := tracker.apply("V", true)
	if !ok {
		t.Fatal("expected first down event")
	}
	if event, ok := tracker.apply("V", true); ok {
		t.Fatalf("expected repeated down to be ignored, got %#v", event)
	}
	up, ok := tracker.apply("V", false)
	if !ok {
		t.Fatal("expected release after first down")
	}
	secondDown, ok := tracker.apply("V", true)
	if !ok {
		t.Fatal("expected second down event")
	}

	sequences := []uint64{down.Sequence, up.Sequence, secondDown.Sequence}
	want := []uint64{1, 2, 3}
	for index := range want {
		if sequences[index] != want[index] {
			t.Fatalf("unexpected sequence at %d: got %d want %d", index, sequences[index], want[index])
		}
	}
}

func TestTransitionTrackerIgnoresNonTargetKey(t *testing.T) {
	tracker := newTransitionTracker("V")

	if event, ok := tracker.apply("B", true); ok {
		t.Fatalf("expected non-target key to be ignored, got %#v", event)
	}
}

func TestRequestThreadQuitRetriesAfterUnhookWhenInitialPostFails(t *testing.T) {
	var calls []string
	posted := requestThreadQuit(42, func(threadID uint32) bool {
		if threadID != 42 {
			t.Fatalf("unexpected thread id: %d", threadID)
		}
		calls = append(calls, "post")
		return len(calls) == 3
	}, func() {
		calls = append(calls, "unhook")
	})

	if !posted {
		t.Fatal("expected retry post to succeed")
	}
	want := []string{"post", "unhook", "post"}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("unexpected call %d: got %q want %q", index, calls[index], want[index])
		}
	}
}

func TestRequestThreadQuitSkipsUnhookWhenInitialPostSucceeds(t *testing.T) {
	unhooked := false
	posted := requestThreadQuit(42, func(uint32) bool { return true }, func() { unhooked = true })

	if !posted {
		t.Fatal("expected initial post to succeed")
	}
	if unhooked {
		t.Fatal("did not expect unhook before the message loop handles WM_QUIT")
	}
}
