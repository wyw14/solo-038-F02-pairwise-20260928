package web

import (
	"fmt"
	"sync"
)

type Segment struct {
	Name      string  `json:"name"`
	Length    float64 `json:"length"`
	Tension   float64 `json:"tension"`
	Available bool    `json:"available"`
}

type Path struct {
	mu       sync.RWMutex
	segments []Segment
}

func NewPath(segments []Segment) (*Path, error) {
	if len(segments) < 3 {
		return nil, fmt.Errorf("web path requires at least three segments")
	}
	for _, segment := range segments {
		if segment.Name == "" || segment.Length <= 0 {
			return nil, fmt.Errorf("web segment is invalid")
		}
	}
	return &Path{segments: append([]Segment(nil), segments...)}, nil
}

// Update changes a segment's tension and availability and returns the
// updated segment so callers can react to the new state immediately.
func (path *Path) Update(name string, tension float64, available bool) (Segment, error) {
	path.mu.Lock()
	defer path.mu.Unlock()
	for index := range path.segments {
		if path.segments[index].Name == name {
			path.segments[index].Tension = tension
			path.segments[index].Available = available
			return path.segments[index], nil
		}
	}
	return Segment{}, fmt.Errorf("web segment %s was not found", name)
}

// Blocked returns the first segment that cannot carry the web, if any.
func (path *Path) Blocked() (Segment, bool) {
	path.mu.RLock()
	defer path.mu.RUnlock()
	for _, segment := range path.segments {
		if !segment.Available || segment.Tension <= 0 {
			return segment, true
		}
	}
	return Segment{}, false
}

func (path *Path) Ready() bool {
	path.mu.RLock()
	defer path.mu.RUnlock()
	for _, segment := range path.segments {
		if !segment.Available || segment.Tension <= 0 {
			return false
		}
	}
	return true
}

func (path *Path) Segments() []Segment {
	path.mu.RLock()
	defer path.mu.RUnlock()
	return append([]Segment(nil), path.segments...)
}
