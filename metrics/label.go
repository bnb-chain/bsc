package metrics

import "encoding/json"

// Label hold an map[string]interface{} value that can be set arbitrarily.
type Label interface {
	Value() map[string]interface{}
	String() string
	Mark(map[string]interface{})
}

// NewRegisteredLabel constructs and registers a new StandardLabel.
func NewRegisteredLabel(name string, r Registry) Label {
	c := NewStandardLabel()
	if nil == r {
		r = DefaultRegistry
	}
	r.Register(name, c)
	return c
}

// NewStandardLabel constructs a new StandardLabel.
func NewStandardLabel() *StandardLabel {
	return &StandardLabel{}
}

// StandardLabel is the standard implementation of a Label.
type StandardLabel struct {
	value   map[string]interface{}
	jsonStr string
}

// Value returns label values.
func (l *StandardLabel) Value() map[string]interface{} {
	return l.value
}

// Mark records the label.
func (l *StandardLabel) Mark(value map[string]interface{}) {
	buf, _ := json.Marshal(value)
	l.jsonStr = string(buf)
	l.value = value
}

// String returns label by JSON format.
func (l *StandardLabel) String() string {
	return l.jsonStr
}
