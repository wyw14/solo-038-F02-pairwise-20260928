package service

import (
	"fmt"

	"github.com/wyw14/solo-038/internal/journal"
	"github.com/wyw14/solo-038/internal/model"
	"github.com/wyw14/solo-038/internal/unwind"
	"github.com/wyw14/solo-038/internal/web"
)

type RecoveryTrace struct {
	Steps           []string `json:"steps"`
	FenceLoaded     bool     `json:"fence_loaded"`
	FeedStarted     bool     `json:"feed_started"`
	RollGeneration  uint64   `json:"roll_generation"`
	BlockedSegment  string   `json:"blocked_segment,omitempty"`
	RecheckSequence uint64   `json:"recheck_sequence"`
}

func (runtime *Runtime) RecoverFeed(recovered journal.RecoveredState, speed float64) (RecoveryTrace, error) {
	decision, err := runtimeRecoveryDecision(recovered)
	if err != nil {
		return RecoveryTrace{}, err
	}
	trace := RecoveryTrace{RollGeneration: decision.Job.RollGeneration}
	webRecord := web.Capture(runtime.web)
	webRecord.Owner = decision.RecoveredOwner
	webRecord.JobGeneration = decision.Job.Generation
	webRecord.RollGeneration = decision.Job.RollGeneration
	webRecord.Threaded = true
	if webRecord.Blockage != nil {
		trace.BlockedSegment = webRecord.Blockage.Segment
	}
	trace.RecheckSequence = webRecord.Recheck.Sequence
	if err := web.Restore(runtime.web, webRecord); err != nil {
		return trace, err
	}
	trace.Steps = append(trace.Steps, "web_fence_loaded")
	trace.FenceLoaded = true
	unwindRecord := unwind.RecoveryRecord{Roll: "recovered-roll", RollGeneration: decision.Job.RollGeneration, Owner: decision.RecoveredOwner, Speed: 0}
	if err := unwind.Restore(runtime.unwind, unwindRecord); err != nil {
		return trace, err
	}
	trace.Steps = append(trace.Steps, "unwind_state_loaded")
	if decision.MayFeed && webRecord.Blockage == nil {
		if err := unwind.ResumeAtSafeSpeed(runtime.unwind, decision.RecoveredOwner, decision.Job.RollGeneration, speed, 15); err != nil {
			return trace, err
		}
		trace.Steps = append(trace.Steps, "feed_started")
		trace.FeedStarted = true
	}
	return trace, nil
}

func runtimeRecoveryDecision(recovered journal.RecoveredState) (recoveryDecision, error) {
	if err := recovered.Job.Validate(); err != nil {
		return recoveryDecision{}, err
	}
	if recovered.Owner == "" {
		return recoveryDecision{}, fmt.Errorf("recovered web owner is required")
	}
	return recoveryDecision{Job: recovered.Job, RecoveredOwner: recovered.Owner, MayFeed: recovered.Job.RollGeneration > 0}, nil
}

type recoveryDecision struct {
	Job            model.Job
	RecoveredOwner string
	MayFeed        bool
}
