package web

import (
	"fmt"
	"sync"
	"time"
)

// Blockage records which segment stopped the web and when.
type Blockage struct {
	Segment string    `json:"segment"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

// RecheckRecord identifies the full-path recheck that released a blockage.
type RecheckRecord struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	Released bool      `json:"released"`
}

type State struct {
	mu             sync.RWMutex
	owner          string
	jobGeneration  uint64
	rollGeneration uint64
	threaded       bool
	meter          float64
	blockage       *Blockage
	recheck        RecheckRecord
}

func NewState() *State {
	return &State{}
}

func (state *State) Fence(owner string, jobGeneration, rollGeneration uint64) error {
	if owner == "" || jobGeneration == 0 || rollGeneration == 0 {
		return fmt.Errorf("web fence identity is incomplete")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.owner = owner
	state.jobGeneration = jobGeneration
	state.rollGeneration = rollGeneration
	state.threaded = false
	state.meter = 0
	state.blockage = nil
	state.recheck = RecheckRecord{}
	return nil
}

func (state *State) Thread(owner string) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner {
		return fmt.Errorf("web owner fence rejected")
	}
	state.threaded = true
	return nil
}

// Block stops further feeding at the current roll meter. It is idempotent:
// the first blockage is kept as the recorded cause until a recheck releases
// it, and later breaks only show up in the path segments themselves.
func (state *State) Block(owner, segment, reason string, now time.Time) error {
	if segment == "" {
		return fmt.Errorf("web blockage segment is required")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner {
		return fmt.Errorf("web owner fence rejected")
	}
	if state.blockage != nil {
		return nil
	}
	state.blockage = &Blockage{Segment: segment, Reason: reason, At: now.UTC()}
	return nil
}

// Recheck releases the blockage after a full-path recheck. The threaded flag
// and roll meter are untouched so feeding resumes at the original meter. It is
// a no-op when the path is already clear.
func (state *State) Recheck(owner string, now time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner {
		return fmt.Errorf("web owner fence rejected")
	}
	if state.blockage == nil {
		return nil
	}
	state.recheck = RecheckRecord{Sequence: state.recheck.Sequence + 1, At: now.UTC(), Released: true}
	state.blockage = nil
	return nil
}

// EnsureClear reports the active blockage, if any.
func (state *State) EnsureClear() error {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.blockage != nil {
		return fmt.Errorf("web feed is blocked by segment %s", state.blockage.Segment)
	}
	return nil
}

func (state *State) Advance(owner string, meters float64) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner || !state.threaded || meters < 0 {
		return fmt.Errorf("web advance is not permitted")
	}
	if state.blockage != nil {
		return fmt.Errorf("web advance is blocked by segment %s", state.blockage.Segment)
	}
	state.meter += meters
	return nil
}

func (state *State) Snapshot() (string, uint64, uint64, bool, float64, *Blockage, RecheckRecord) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	var blockage *Blockage
	if state.blockage != nil {
		copied := *state.blockage
		blockage = &copied
	}
	return state.owner, state.jobGeneration, state.rollGeneration, state.threaded, state.meter, blockage, state.recheck
}
