package web

import (
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

type Coordinator struct {
	state *State
	path  *Path
}

func NewCoordinator(state *State, path *Path) *Coordinator {
	return &Coordinator{state: state, path: path}
}

func (coordinator *Coordinator) Prepare(owner string, jobGeneration, rollGeneration uint64) error {
	if err := coordinator.state.Fence(owner, jobGeneration, rollGeneration); err != nil {
		return err
	}
	if !coordinator.path.Ready() {
		return nil
	}
	return coordinator.state.Thread(owner)
}

func (coordinator *Coordinator) Permit(readings []sensor.Reading, tolerance float64, now time.Time) (TensionPermit, error) {
	return BuildTensionPermit(readings, tolerance, now)
}

// MarkSegment updates a segment's tension and availability. If the update
// leaves the segment unable to carry the web while a job is fenced, that job
// is blocked at its current meter immediately.
func (coordinator *Coordinator) MarkSegment(name string, tension float64, available bool, now time.Time) (Segment, error) {
	segment, err := coordinator.path.Update(name, tension, available)
	if err != nil {
		return Segment{}, err
	}
	if !segment.Available || segment.Tension <= 0 {
		reason := "segment marked unavailable"
		if segment.Tension <= 0 && segment.Available {
			reason = "segment tension is not positive"
		}
		if owner, _, _, _, _, _, _ := coordinator.state.Snapshot(); owner != "" {
			if err := coordinator.state.Block(owner, name, reason, now); err != nil {
				return segment, err
			}
		}
	}
	return segment, nil
}

// RecheckPath verifies every segment across the full path and releases the
// blockage only when the whole path is ready. A remaining blocked segment
// keeps the job stopped.
func (coordinator *Coordinator) RecheckPath(now time.Time) (RecheckRecord, error) {
	if blocked, isBlocked := coordinator.path.Blocked(); isBlocked {
		return RecheckRecord{}, &PathBlockedError{Segment: blocked.Name}
	}
	owner, _, _, _, _, _, _ := coordinator.state.Snapshot()
	if owner == "" {
		return RecheckRecord{}, nil
	}
	if err := coordinator.state.Recheck(owner, now); err != nil {
		return RecheckRecord{}, err
	}
	_, _, _, _, _, _, recheck := coordinator.state.Snapshot()
	return recheck, nil
}

// EnsureClear reports whether the running job may feed right now.
func (coordinator *Coordinator) EnsureClear() error {
	return coordinator.state.EnsureClear()
}

// Path returns the live runtime path segments.
func (coordinator *Coordinator) Path() []Segment {
	return coordinator.path.Segments()
}

// Blockage reports the segment currently stopping the web, if any.
func (coordinator *Coordinator) Blockage() (*Blockage, RecheckRecord) {
	_, _, _, _, _, blockage, recheck := coordinator.state.Snapshot()
	return blockage, recheck
}

func (coordinator *Coordinator) Advance(owner string, meters float64, permit TensionPermit, now time.Time) error {
	if err := coordinator.state.EnsureClear(); err != nil {
		return err
	}
	if !permit.ValidAt(now) {
		return &PermitError{Reason: permit.Reason}
	}
	return coordinator.state.Advance(owner, meters)
}

type PermitError struct {
	Reason string
}

func (err *PermitError) Error() string {
	if err.Reason == "" {
		return "web permit is not valid"
	}
	return "web permit is not valid: " + err.Reason
}

// PathBlockedError reports the segment that failed a full-path recheck.
type PathBlockedError struct {
	Segment string
}

func (err *PathBlockedError) Error() string {
	return "web path recheck failed: segment " + err.Segment + " is blocked"
}
