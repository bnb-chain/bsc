package metrics

// Label is the standard implementation of a Label.
type Label struct {
	value map[string]interface{}
}

// GetOrRegisterLabel returns an existing Label or constructs and registers a
// new Label.
func GetOrRegisterLabel(name string, r Registry) *Label {
	if r == nil {
		r = DefaultRegistry
	}
	return r.GetOrRegister(name, NewLabel).(*Label)
}

// NewLabel constructs a new Label.
func NewLabel() *Label {
	return &Label{value: make(map[string]interface{})}
}

// Value returns label values.
func (l *Label) Value() map[string]interface{} {
	return l.value
}

// Mark records the label.
func (l *Label) Mark(value map[string]interface{}) {
	for k, v := range value {
		l.value[k] = v
	}
}
