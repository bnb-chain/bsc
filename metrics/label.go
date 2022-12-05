package metrics

// Label hold an map[string]interface{} value that can be set arbitrarily.
type Label interface {
	Value() map[string]interface{}
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
	value map[string]interface{}
}

// Value returns label values.
func (l *StandardLabel) Value() map[string]interface{} {
	return l.value
}

// Mark records the label.
func (l *StandardLabel) Mark(value map[string]interface{}) {
	l.value = value
}
