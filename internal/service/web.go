package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/wyw14/solo-038/internal/model"
	"github.com/wyw14/solo-038/internal/unwind"
	"github.com/wyw14/solo-038/internal/web"
)

// WebBlock records which segment interrupted feeding and where the web stopped.
type WebBlock struct {
	Segment   string    `json:"segment"`
	Meter     float64   `json:"meter"`
	BlockedAt time.Time `json:"blocked_at"`
}

// WebRecovery records a release: which segment had blocked the path and which
// recheck released it, so the resume position is auditable.
type WebRecovery struct {
	ID             model.EventID `json:"id"`
	BlockedSegment string        `json:"blocked_segment"`
	RecheckID      uint64        `json:"recheck_id"`
	RollGeneration uint64        `json:"roll_generation"`
	Meter          float64       `json:"meter"`
	ResumedAt      time.Time     `json:"resumed_at"`
}

var webMu sync.Mutex

func (runtime *Runtime) WebPath() []web.Segment {
	return runtime.webControl.Segments()
}

func (runtime *Runtime) WebBlock() (WebBlock, bool) {
	webMu.Lock()
	defer webMu.Unlock()
	if runtime.webBlock == nil {
		return WebBlock{}, false
	}
	return *runtime.webBlock, true
}

func (runtime *Runtime) WebRecoveries() []WebRecovery {
	webMu.Lock()
	defer webMu.Unlock()
	return append([]WebRecovery(nil), runtime.webRecoveries...)
}

// UpdateWebSegment applies a segment state change to the live path. If the
// change blocks the path, feeding stops immediately rather than on the next
// feed attempt.
func (runtime *Runtime) UpdateWebSegment(name string, tension float64, available bool) error {
	if err := runtime.webControl.UpdateSegment(name, tension, available); err != nil {
		return err
	}
	if segment, blocked := runtime.webControl.BlockedSegment(); blocked {
		runtime.noteWebBlock(segment, time.Now())
	}
	return nil
}

// noteWebBlock stops the unwind feed and remembers which segment blocked the
// path. The web meter is left untouched so a later resume continues at the
// same roll position.
func (runtime *Runtime) noteWebBlock(segment string, now time.Time) {
	_, _, feedOwner, _ := runtime.unwind.Snapshot()
	if feedOwner != "" {
		runtime.unwind.StopFeed(feedOwner)
	}
	_, _, _, _, meter := runtime.web.Snapshot()
	webMu.Lock()
	fresh := runtime.webBlock == nil
	if fresh {
		runtime.webBlock = &WebBlock{Segment: segment, Meter: meter, BlockedAt: now.UTC()}
	}
	webMu.Unlock()
	if fresh {
		runtime.RecordIncident("error", "web", fmt.Sprintf("web segment %s blocked; feed stopped at %.1f m", segment, meter))
	}
}

// resumeWebOperation rechecks the whole path after a repair and, only if every
// segment is available and tensioned, resumes feeding at the meter where the
// block stopped the web. The recovery record names the blocked segment and
// the recheck that released it.
func (runtime *Runtime) resumeWebOperation(current model.Job) error {
	now := time.Now()
	block, ok := runtime.WebBlock()
	if !ok {
		return fmt.Errorf("web path has no blocked segment to resume from")
	}
	recheck, err := runtime.webControl.RecheckPath(now)
	if err != nil {
		return err
	}
	rollGeneration := current.RollGeneration
	if _, generation, _, _ := runtime.unwind.Snapshot(); generation != 0 {
		rollGeneration = generation
	}
	if err := unwind.ResumeAtSafeSpeed(runtime.unwind, string(current.ID), rollGeneration, 4, 8); err != nil {
		return err
	}
	_, _, _, _, meter := runtime.web.Snapshot()
	record := WebRecovery{ID: model.NewEventID(), BlockedSegment: block.Segment, RecheckID: recheck.ID, RollGeneration: rollGeneration, Meter: meter, ResumedAt: now.UTC()}
	webMu.Lock()
	runtime.webRecoveries = append(runtime.webRecoveries, record)
	runtime.webBlock = nil
	webMu.Unlock()
	runtime.RecordIncident("info", "web", fmt.Sprintf("web segment %s released by recheck %d; feed resumed at %.1f m", record.BlockedSegment, record.RecheckID, record.Meter))
	return nil
}
