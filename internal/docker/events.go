package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Event is a single Docker Engine event (GET /events stream).
type Event struct {
	Type     string            `json:"Type"`
	Action   string            `json:"Action"`
	Actor    EventActor        `json:"actor"`
	Status   string            `json:"status"`
	ID       string            `json:"id"`
	From     string            `json:"from"`
	Time     int64             `json:"time"`
	TimeNano int64             `json:"timeNano"`
}

// EventActor is the actor of a docker event.
type EventActor struct {
	ID         string            `json:"ID"`
	Attributes map[string]string `json:"Attributes"`
}

// ContainerName returns the container name attribute of the event actor, if
// present (docker sets Actor.Attributes.name for container events).
func (e Event) ContainerName() string {
	if e.Actor.Attributes == nil {
		return ""
	}
	return e.Actor.Attributes["name"]
}

// ImageName returns the image reference attribute (e.g. "nginx:latest").
func (e Event) ImageName() string {
	if e.Actor.Attributes == nil {
		return ""
	}
	return e.Actor.Attributes["image"]
}

// StreamEvents reads the docker events stream and sends each parsed event to
// out until ctx is cancelled, the stream ends, or an unrecoverable error
// occurs. The caller owns `out`; StreamEvents closes it when done. since
// selects events after a unix timestamp (0 = from now).
func (c *Client) StreamEvents(ctx context.Context, since int64, out chan<- Event) error {
	defer close(out)

	u := c.base + "/events"
	if since > 0 {
		u += fmt.Sprintf("?since=%d", since)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	// The events endpoint streams forever; keep the connection open.
	req.Header.Set("Accept", "application/json")

	httpc := *c.httpc
	httpc.Timeout = 0 // no overall timeout for a stream
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		discardBody(resp.Body)
		return fmt.Errorf("docker events: status %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return err
			}
			return nil // clean EOF
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// Tolerate malformed lines; keep streaming.
			continue
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return nil
		}
	}
}
