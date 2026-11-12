package push

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"synapse/internal/db"
)

func testRSAKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

func TestBuildServiceAccountJWTClaims(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	aud := "https://oauth2.googleapis.com/token"
	pemKey, key := testRSAKey(t)
	token, err := buildServiceAccountJWT("svc@example.iam.gserviceaccount.com", pemKey, aud, now)
	if err != nil {
		t.Fatalf("build jwt: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}
	decode := func(seg string) map[string]any {
		raw, err := base64.RawURLEncoding.DecodeString(seg)
		if err != nil {
			t.Fatalf("decode segment: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal segment: %v", err)
		}
		return m
	}

	header := decode(parts[0])
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("unexpected header: %+v", header)
	}

	claims := decode(parts[1])
	if claims["iss"] != "svc@example.iam.gserviceaccount.com" {
		t.Errorf("iss = %v", claims["iss"])
	}
	if claims["scope"] != fcmScope {
		t.Errorf("scope = %v", claims["scope"])
	}
	if claims["aud"] != aud {
		t.Errorf("aud = %v", claims["aud"])
	}
	if int64(claims["iat"].(float64)) != now.Unix() {
		t.Errorf("iat = %v, want %d", claims["iat"], now.Unix())
	}
	if int64(claims["exp"].(float64)) != now.Add(time.Hour).Unix() {
		t.Errorf("exp = %v, want %d", claims["exp"], now.Add(time.Hour).Unix())
	}

	// The signature must verify against the public half of the key.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
}

func TestParseServiceAccount(t *testing.T) {
	raw := `{"client_email":"x@y.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nAA==\n-----END PRIVATE KEY-----\n","project_id":"proj"}`
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sa.ClientEmail != "x@y.iam.gserviceaccount.com" || sa.ProjectID != "proj" {
		t.Fatalf("unexpected: %+v", sa)
	}
	if sa.TokenURI != defaultServiceTokenURI {
		t.Fatalf("token uri default = %q", sa.TokenURI)
	}
	if _, err := parseServiceAccount(""); err == nil {
		t.Fatal("expected error for empty service account")
	}
	if _, err := parseServiceAccount(`{"client_email":"x"}`); err == nil {
		t.Fatal("expected error for missing private key")
	}
}

func TestEligible(t *testing.T) {
	all := db.PushPreferences{Enabled: true, WebPush: true, FCM: true, NewItem: true}

	cases := []struct {
		name    string
		prefs   db.PushPreferences
		channel string
		want    bool
	}{
		{"web push allowed", all, "web_push", true},
		{"fcm allowed", all, "fcm", true},
		{"unknown channel", all, "sms", false},
		{"globally disabled", db.PushPreferences{Enabled: false, WebPush: true, FCM: true, NewItem: true}, "web_push", false},
		{"new item off", db.PushPreferences{Enabled: true, WebPush: true, FCM: true, NewItem: false}, "fcm", false},
		{"web push off", db.PushPreferences{Enabled: true, WebPush: false, FCM: true, NewItem: true}, "web_push", false},
		{"fcm off", db.PushPreferences{Enabled: true, WebPush: true, FCM: false, NewItem: true}, "fcm", false},
	}
	for _, tc := range cases {
		if got := eligible(tc.prefs, tc.channel); got != tc.want {
			t.Errorf("%s: eligible = %v, want %v", tc.name, got, tc.want)
		}
	}
}
