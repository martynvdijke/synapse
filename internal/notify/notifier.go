package notify

import (
	"context"
)

// Notifier is implemented by every notification channel (Gotify, ntfy,
// Telegram, Discord, generic webhook, ...). Implementations must be safe for
// sequential use; Send must honor ctx cancellation and return an error when
// delivery failed so the fan-out can isolate failing channels.
type Notifier interface {
	// Name identifies the channel instance in logs and test results.
	Name() string
	// Enabled reports whether the channel is configured enough to attempt sends.
	Enabled() bool
	// Send delivers one message for the given category.
	Send(ctx context.Context, cat Category, title, message string) error
}

// compile-time check that the Gotify client satisfies the contract.
var _ Notifier = (*Client)(nil)

// Name returns the channel name for the Gotify client.
func (c *Client) Name() string { return "gotify" }

// Enabled reports whether the Gotify client has URL and token configured.
func (c *Client) Enabled() bool { return c.url != "" && c.token != "" }

// Send delivers a message via the Gotify client (Notifier implementation).
func (c *Client) Send(ctx context.Context, _ Category, title, message string) error {
	return c.SendMessage(ctx, title, message)
}
