package web

import (
	"sync"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

type Coordinator struct {
	state    *State
	path     *Path
	mu       sync.Mutex
	rechecks uint64
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

func (coordinator *Coordinator) Advance(owner string, meters float64, permit TensionPermit, now time.Time) error {
	if segment, blocked := coordinator.path.BlockedSegment(); blocked {
		return &PathBlockedError{Segment: segment}
	}
	if !permit.ValidAt(now) {
		return &PermitError{Reason: permit.Reason}
	}
	return coordinator.state.Advance(owner, meters)
}

func (coordinator *Coordinator) UpdateSegment(name string, tension float64, available bool) error {
	return coordinator.path.Update(name, tension, available)
}

func (coordinator *Coordinator) Segments() []Segment {
	return coordinator.path.Segments()
}

func (coordinator *Coordinator) BlockedSegment() (string, bool) {
	return coordinator.path.BlockedSegment()
}

func (coordinator *Coordinator) RecheckPath(now time.Time) (PathRecheck, error) {
	segments := coordinator.path.Segments()
	for _, segment := range segments {
		if !segment.Available || segment.Tension <= 0 {
			return PathRecheck{}, &PathBlockedError{Segment: segment.Name}
		}
	}
	coordinator.mu.Lock()
	coordinator.rechecks++
	id := coordinator.rechecks
	coordinator.mu.Unlock()
	return PathRecheck{ID: id, Segments: len(segments), CheckedAt: now.UTC()}, nil
}

type PathRecheck struct {
	ID        uint64    `json:"id"`
	Segments  int       `json:"segments"`
	CheckedAt time.Time `json:"checked_at"`
}

type PathBlockedError struct {
	Segment string
}

func (err *PathBlockedError) Error() string {
	return "web path is blocked at segment " + err.Segment
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
