package push

import (
	"encoding/json"
	"time"
)

// Event is the realtime notification delivered over SSE and stored/forwarded
// to push channels. It is serialized as-is on the event stream.
type Event struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	URL      string    `json:"url"`
	Category string    `json:"category"`
	At       time.Time `json:"at"`
}

// Payload is the channel-agnostic notification content handed to Web Push and
// FCM. Data carries channel-specific extra fields (for Android, channel_id).
type Payload struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	URL      string            `json:"url"`
	Tag      string            `json:"tag"`
	Category string            `json:"category"`
	Data     map[string]string `json:"data,omitempty"`
}

// JSON serializes the payload for Web Push delivery.
func (p Payload) JSON() ([]byte, error) {
	return json.Marshal(p)
}
