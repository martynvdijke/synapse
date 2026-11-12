package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"synapse/internal/db"
)

// ErrDeviceGone marks an FCM token Google reports as unregistered/not found.
// Callers prune such devices.
var ErrDeviceGone = errors.New("fcm device gone")

const (
	fcmScope               = "https://www.googleapis.com/auth/firebase.messaging"
	defaultServiceTokenURI = "https://oauth2.googleapis.com/token"
	fcmSendEndpoint        = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
)

// serviceAccount is the subset of a Google service-account JSON document we
// need. PrivateKey is a PEM-encoded RSA key.
type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

func parseServiceAccount(raw string) (*serviceAccount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("fcm service account not configured")
	}
	var sa serviceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return nil, fmt.Errorf("parse fcm service account: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm service account missing client_email or private_key")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = defaultServiceTokenURI
	}
	return &sa, nil
}

// buildServiceAccountJWT builds the RS256-signed OAuth2 assertion used for the
// JWT-bearer token exchange. Pure function so the claim shape can be tested
// offline.
func buildServiceAccountJWT(iss, privateKeyPEM, aud string, now time.Time) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   iss,
		"scope": fcmScope,
		"aud":   aud,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding
	signingInput := b64.EncodeToString(headerJSON) + "." + b64.EncodeToString(claimsJSON)

	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + b64.EncodeToString(sig), nil
}

func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(pemStr, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("fcm private key is not valid PEM")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("fcm private key is not RSA")
		}
		return key, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// exchangeServiceAccountToken trades a signed assertion for an OAuth2 access
// token. Returns the token and its lifetime.
func exchangeServiceAccountToken(ctx context.Context, hc *http.Client, sa *serviceAccount) (string, time.Duration, error) {
	assertion, err := buildServiceAccountJWT(sa.ClientEmail, sa.PrivateKey, sa.TokenURI, time.Now())
	if err != nil {
		return "", 0, err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("fcm token exchange: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", 0, fmt.Errorf("fcm token exchange: %w", err)
	}
	if out.AccessToken == "" {
		return "", 0, errors.New("fcm token exchange returned no access token")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	return out.AccessToken, ttl, nil
}

// fcmAccessToken returns a cached access token, refreshing it shortly before
// expiry.
func (n *Notifier) fcmAccessToken(ctx context.Context, sa *serviceAccount) (string, error) {
	n.fcmMu.Lock()
	if n.fcmToken != "" && time.Now().Before(n.fcmExpiry.Add(-60*time.Second)) {
		tok := n.fcmToken
		n.fcmMu.Unlock()
		return tok, nil
	}
	n.fcmMu.Unlock()

	token, ttl, err := exchangeServiceAccountToken(ctx, n.httpClient, sa)
	if err != nil {
		return "", err
	}
	n.fcmMu.Lock()
	n.fcmToken = token
	n.fcmExpiry = time.Now().Add(ttl)
	n.fcmMu.Unlock()
	return token, nil
}

type fcmErrorBody struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func (e fcmErrorBody) gone() bool {
	if e.Error.Status == "UNREGISTERED" || e.Error.Status == "NOT_FOUND" {
		return true
	}
	for _, d := range e.Error.Details {
		if d.ErrorCode == "UNREGISTERED" || d.ErrorCode == "NOT_FOUND" {
			return true
		}
	}
	return false
}

// sendFCM delivers one payload to one device token.
func (n *Notifier) sendFCM(ctx context.Context, s db.Settings, device db.PushDevice, payload Payload) error {
	sa, err := parseServiceAccount(s.FCMServiceAccountJSON)
	if err != nil {
		return err
	}
	projectID := strings.TrimSpace(s.FCMProjectID)
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return errors.New("fcm project id not configured")
	}
	channelID := strings.TrimSpace(s.FCMAndroidChannelID)
	if channelID == "" {
		channelID = "synapse_general"
	}
	data := map[string]string{}
	for k, v := range payload.Data {
		data[k] = v
	}
	data["channel_id"] = channelID

	token, err := n.fcmAccessToken(ctx, sa)
	if err != nil {
		return err
	}

	message := map[string]any{
		"message": map[string]any{
			"token":        device.Token,
			"notification": map[string]string{"title": payload.Title, "body": payload.Body},
			"data":         data,
			"android": map[string]any{
				"priority":     "high",
				"notification": map[string]string{"channel_id": channelID},
			},
		},
	}
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(fcmSendEndpoint, projectID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var fb fcmErrorBody
	_ = json.Unmarshal(respBody, &fb)
	if fb.gone() || resp.StatusCode == http.StatusNotFound {
		return ErrDeviceGone
	}
	return fmt.Errorf("fcm send: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}
