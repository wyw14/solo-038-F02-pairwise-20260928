package web

import (
	"strings"
	"testing"
	"time"
)

func fencedState(t *testing.T) *State {
	t.Helper()
	state := NewState()
	if err := state.Fence("job-1", 1, 1); err != nil {
		t.Fatalf("fence: %v", err)
	}
	if err := state.Thread("job-1"); err != nil {
		t.Fatalf("thread: %v", err)
	}
	return state
}

func TestBlockStopsAdvanceAndRecheckResumesAtSameMeter(t *testing.T) {
	state := fencedState(t)
	if err := state.Advance("job-1", 12.5); err != nil {
		t.Fatalf("advance before block: %v", err)
	}
	now := time.Now()
	if err := state.Block("job-1", "press", "segment marked unavailable", now); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := state.Advance("job-1", 1); err == nil || !strings.Contains(err.Error(), "press") {
		t.Fatalf("advance while blocked should name the segment, got %v", err)
	}
	if err := state.EnsureClear(); err == nil {
		t.Fatalf("ensure clear should fail while blocked")
	}
	if err := state.Recheck("job-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if err := state.Advance("job-1", 1); err != nil {
		t.Fatalf("advance after recheck: %v", err)
	}
	_, _, _, threaded, meter, blockage, recheck := state.Snapshot()
	if !threaded || meter != 13.5 {
		t.Fatalf("expected threading kept and meter resumed at 13.5, got threaded=%v meter=%v", threaded, meter)
	}
	if blockage != nil {
		t.Fatalf("blockage should be released, got %+v", blockage)
	}
	if recheck.Sequence != 1 || !recheck.Released {
		t.Fatalf("expected recheck #1 released, got %+v", recheck)
	}
}

func TestBlockKeepsFirstSegment(t *testing.T) {
	state := fencedState(t)
	now := time.Now()
	if err := state.Block("job-1", "press", "first", now); err != nil {
		t.Fatalf("first block: %v", err)
	}
	if err := state.Block("job-1", "dryer", "second", now); err != nil {
		t.Fatalf("second block should be idempotent: %v", err)
	}
	_, _, _, _, _, blockage, _ := state.Snapshot()
	if blockage == nil || blockage.Segment != "press" {
		t.Fatalf("expected first blockage press kept, got %+v", blockage)
	}
}

func TestRecheckWithoutBlockageIsNoOp(t *testing.T) {
	state := fencedState(t)
	if err := state.Recheck("job-1", time.Now()); err != nil {
		t.Fatalf("recheck without blockage: %v", err)
	}
	_, _, _, _, _, _, recheck := state.Snapshot()
	if recheck.Sequence != 0 {
		t.Fatalf("recheck without blockage must not count, got %+v", recheck)
	}
}

func TestRecoveryRoundTripKeepsBlockageAndRecheck(t *testing.T) {
	state := fencedState(t)
	now := time.Now()
	if err := state.Advance("job-1", 7); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if err := state.Block("job-1", "dryer", "segment marked unavailable", now); err != nil {
		t.Fatalf("block: %v", err)
	}
	record := Capture(state)
	restored := NewState()
	if err := Restore(restored, record); err != nil {
		t.Fatalf("restore: %v", err)
	}
	owner, _, _, _, meter, blockage, _ := restored.Snapshot()
	if owner != "job-1" || meter != 7 {
		t.Fatalf("expected owner job-1 at meter 7, got %s at %.1f", owner, meter)
	}
	if blockage == nil || blockage.Segment != "dryer" {
		t.Fatalf("expected restored blockage dryer, got %+v", blockage)
	}
	if err := restored.Recheck("job-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("recheck after restore: %v", err)
	}
	_, _, _, _, _, _, recheck := restored.Snapshot()
	if recheck.Sequence != 1 || !recheck.Released {
		t.Fatalf("expected recheck #1 after restore, got %+v", recheck)
	}
}

func TestCoordinatorRecheckPathRequiresFullPathReady(t *testing.T) {
	state := fencedState(t)
	path, err := NewPath([]Segment{
		{Name: "unwind", Length: 4, Tension: 8, Available: true},
		{Name: "press", Length: 12, Tension: 8, Available: true},
		{Name: "dryer", Length: 18, Tension: 7, Available: true},
	})
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	coordinator := NewCoordinator(state, path)
	now := time.Now()
	if _, err := coordinator.MarkSegment("press", 0, false, now); err != nil {
		t.Fatalf("break press: %v", err)
	}
	if _, err := coordinator.MarkSegment("dryer", 0, false, now); err != nil {
		t.Fatalf("break dryer: %v", err)
	}
	if _, err := coordinator.MarkSegment("press", 8, true, now); err != nil {
		t.Fatalf("repair press: %v", err)
	}
	if _, err := coordinator.RecheckPath(now); err == nil || !strings.Contains(err.Error(), "dryer") {
		t.Fatalf("recheck should fail on dryer, got %v", err)
	}
	if _, err := coordinator.MarkSegment("dryer", 7, true, now); err != nil {
		t.Fatalf("repair dryer: %v", err)
	}
	recheck, err := coordinator.RecheckPath(now)
	if err != nil {
		t.Fatalf("recheck after full repair: %v", err)
	}
	if recheck.Sequence != 1 || !recheck.Released {
		t.Fatalf("expected recheck #1 released, got %+v", recheck)
	}
	if err := coordinator.EnsureClear(); err != nil {
		t.Fatalf("path should be clear after recheck: %v", err)
	}
}
