package securityaudit

// EventType identifies the class of security-relevant event.
type EventType string

const (
	// EventInsecureBypass is emitted when an insecure runtime override is used.
	EventInsecureBypass EventType = "insecure_bypass"
)

// Event is a structured security-relevant event.
type Event struct {
	Type        EventType
	Component   string
	Name        string
	Description string
	Attrs       map[string]string
}

// Sink handles security events.
type Sink interface {
	HandleSecurityEvent(Event)
}

var sink Sink

// SetSink configures the global event sink.
func SetSink(s Sink) {
	sink = s
}

// Emit publishes a security event to the configured sink, if any.
func Emit(evt Event) {
	if sink != nil {
		sink.HandleSecurityEvent(evt)
	}
}
