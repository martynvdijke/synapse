package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"

	"synapse/internal/db"
)

// ErrSubscriptionGone marks a Web Push endpoint the push service has dropped
// (HTTP 404/410). Callers prune such subscriptions.
var ErrSubscriptionGone = errors.New("push subscription gone")

// GenerateVAPIDKeys creates a VAPID key pair in base64url form. The public key
// is what the browser subscribes with; the private key stays server-side.
func GenerateVAPIDKeys() (public, private string, err error) {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	return pub, priv, nil
}

// webPushSubject normalizes the configured subject for webpush-go. That
// library prepends "mailto:" unless the value is an https URL, so an already
// prefixed mailto: value must be stripped to avoid "mailto:mailto:".
func webPushSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "admin@localhost"
	}
	if strings.HasPrefix(subject, "https:") {
		return subject
	}
	return strings.TrimPrefix(subject, "mailto:")
}

// sendWebPush delivers one payload to a browser subscription.
func sendWebPush(ctx context.Context, hc *http.Client, s db.Settings, sub db.PushSubscription, payload Payload) error {
	body, err := payload.JSON()
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256DH,
			Auth:   sub.Auth,
		},
	}, &webpush.Options{
		HTTPClient:      hc,
		Subscriber:      webPushSubject(s.WebPushSubject),
		TTL:             86400,
		VAPIDPublicKey:  s.WebPushVAPIDPublicKey,
		VAPIDPrivateKey: s.WebPushVAPIDPrivateKey,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrSubscriptionGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("web push: unexpected status %d", resp.StatusCode)
	}
	return nil
}
