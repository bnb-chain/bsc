package metrics

import (
	"maps"
	"sync"
)

// LabelValue is a mapping of keys to values
type LabelValue map[string]any

// LabelSnapshot is a read-only copy of a Label.
type LabelSnapshot LabelValue

// Value returns the value at the time the snapshot was taken.
func (l LabelSnapshot) Value() LabelValue { return LabelValue(l) }

// Label is the standard implementation of a Label.
type Label struct {
	value LabelValue

	mutex sync.Mutex
}

// GetOrRegisterLabel returns an existing Label or constructs and registers a
// new Label.
func GetOrRegisterLabel(name string, r Registry) *Label {
	return getOrRegister(name, NewLabel, r)
}

// NewLabel constructs a new Label.
func NewLabel() *Label {
	return &Label{value: make(map[string]any)}
}

// Value returns label values.
func (l *Label) Snapshot() *LabelSnapshot {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	snapshot := LabelSnapshot(maps.Clone(l.value))
	return &snapshot
}

// Mark records the label.
func (l *Label) Mark(value map[string]interface{}) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	maps.Copy(l.value, value)
}
