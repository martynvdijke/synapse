package channels

import (
	"testing"

	"synapse/internal/notify"
)

func TestParseChannelsEmpty(t *testing.T) {
	cfgs, err := ParseChannels("")
	if err != nil || cfgs != nil {
		t.Fatalf("empty doc should yield nil,nil; got %v,%v", cfgs, err)
	}
	cfgs, err = ParseChannels("   ")
	if err != nil || cfgs != nil {
		t.Fatalf("blank doc should yield nil,nil; got %v,%v", cfgs, err)
	}
}

func TestParseChannelsValid(t *testing.T) {
	doc := `[
		{"type":"ntfy","enabled":true,"url":"https://ntfy.example.com/topic","priority":4},
		{"type":"telegram","enabled":false,"url":"https://api.telegram.org/botTOK/123"},
		{"type":"discord","enabled":true,"url":"https://discord.com/api/webhooks/1/x"},
		{"type":"webhook","enabled":true,"url":"https://hook.example.com/synapse","token":"s3cret"}
	]`
	cfgs, err := ParseChannels(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 4 {
		t.Fatalf("want 4 channels, got %d", len(cfgs))
	}
	if cfgs[0].Type != TypeNtfy || !cfgs[0].Enabled || cfgs[0].Priority != 4 {
		t.Fatalf("ntfy entry parsed wrong: %+v", cfgs[0])
	}
	if cfgs[3].Token != "s3cret" {
		t.Fatalf("webhook token lost: %+v", cfgs[3])
	}
}

func TestParseChannelsUnknownType(t *testing.T) {
	if _, err := ParseChannels(`[{"type":"pagerduty","enabled":true,"url":"x"}]`); err == nil {
		t.Fatal("unknown type must be rejected")
	}
}

func TestParseChannelsInvalidJSON(t *testing.T) {
	if _, err := ParseChannels("{not json"); err == nil {
		t.Fatal("invalid JSON must be rejected")
	}
}

func TestBuildAllSkipsDisabledAndUnknown(t *testing.T) {
	cfgs := []Config{
		{Type: TypeGotify, Enabled: true, URL: "http://g", Token: "t"},
		{Type: TypeNtfy, Enabled: false, URL: "http://n"},
		{Type: "bogus", Enabled: true},
	}
	built, errs := BuildAll(cfgs)
	if len(built) != 1 {
		t.Fatalf("want 1 built channel, got %d", len(built))
	}
	if built[0].Name() != TypeGotify || !built[0].Enabled() {
		t.Fatalf("gotify channel wrong: %v enabled=%v", built[0].Name(), built[0].Enabled())
	}
	if len(errs) != 1 {
		t.Fatalf("want 1 build error (bogus), got %d", len(errs))
	}
}

func TestGotifyImplementsNotifier(t *testing.T) {
	var _ notify.Notifier = notify.NewClient("http://g", "tok", 5)
}
