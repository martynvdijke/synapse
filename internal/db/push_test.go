package db

import (
	"testing"
)

func TestPushPreferencesDefaultsAndRoundTrip(t *testing.T) {
	database := setupTestDB(t)

	prefs, err := database.GetPushPreferences(42)
	if err != nil {
		t.Fatalf("get defaults: %v", err)
	}
	if !prefs.Enabled || !prefs.WebPush || !prefs.FCM || !prefs.NewItem {
		t.Fatalf("expected all-true defaults, got %+v", prefs)
	}
	if prefs.UserID != 42 {
		t.Fatalf("user id = %d, want 42", prefs.UserID)
	}

	prefs.Enabled = false
	prefs.WebPush = true
	prefs.FCM = false
	prefs.NewItem = false
	if err := database.SavePushPreferences(prefs); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := database.GetPushPreferences(42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled || !got.WebPush || got.FCM || got.NewItem {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at to be set")
	}

	// Upsert updates in place rather than inserting a second row.
	got.Enabled = true
	if err := database.SavePushPreferences(got); err != nil {
		t.Fatalf("second save: %v", err)
	}
	var count int
	if err := database.rawDB.QueryRow("SELECT COUNT(*) FROM push_preferences").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 prefs row, got %d", count)
	}
}

func TestPushSubscriptionLifecycle(t *testing.T) {
	database := setupTestDB(t)

	sub := &PushSubscription{UserID: 1, Endpoint: "https://push.example/abc", P256DH: "pk", Auth: "au", UserAgent: "test"}
	id, err := database.UpsertPushSubscription(sub)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id == 0 || sub.ID != id {
		t.Fatalf("expected non-zero id, got %d (sub.ID %d)", id, sub.ID)
	}

	// Re-registering the same endpoint updates keys and owner in place.
	sub2 := &PushSubscription{UserID: 2, Endpoint: "https://push.example/abc", P256DH: "pk2", Auth: "au2", UserAgent: "ua2"}
	id2, err := database.UpsertPushSubscription(sub2)
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if id2 != id {
		t.Fatalf("endpoint conflict should keep id %d, got %d", id, id2)
	}

	list, err := database.ListPushSubscriptions(2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].P256DH != "pk2" || list[0].Auth != "au2" || list[0].UserAgent != "ua2" {
		t.Fatalf("unexpected subscriptions: %+v", list)
	}
	if other, _ := database.ListPushSubscriptions(1); len(other) != 0 {
		t.Fatalf("old owner still has subscription: %+v", other)
	}
	if all, _ := database.ListAllPushSubscriptions(); len(all) != 1 {
		t.Fatalf("expected 1 subscription total, got %d", len(all))
	}

	// Endpoint deletion is owner-scoped.
	if err := database.DeletePushSubscriptionByEndpoint(1, "https://push.example/abc"); err != nil {
		t.Fatalf("delete wrong owner: %v", err)
	}
	if all, _ := database.ListAllPushSubscriptions(); len(all) != 1 {
		t.Fatalf("wrong-owner delete removed subscription")
	}
	if err := database.DeletePushSubscriptionByEndpoint(2, "https://push.example/abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if all, _ := database.ListAllPushSubscriptions(); len(all) != 0 {
		t.Fatalf("subscription not deleted")
	}
}

func TestPushDeviceLifecycle(t *testing.T) {
	database := setupTestDB(t)

	dev := &PushDevice{UserID: 7, Token: "tok-1", Platform: "android", DeviceName: "Pixel"}
	id, err := database.UpsertPushDevice(dev)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// Same token, different owner/platform updates in place.
	dev2 := &PushDevice{UserID: 8, Token: "tok-1", Platform: "android", DeviceName: "Pixel 8"}
	id2, err := database.UpsertPushDevice(dev2)
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if id2 != id {
		t.Fatalf("token conflict should keep id %d, got %d", id, id2)
	}

	list, err := database.ListPushDevices(8)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].DeviceName != "Pixel 8" {
		t.Fatalf("unexpected devices: %+v", list)
	}
	if all, _ := database.ListAllPushDevices(); len(all) != 1 {
		t.Fatalf("expected 1 device total, got %d", len(all))
	}

	before := list[0].LastSeen
	if err := database.TouchPushDevice("tok-1"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	list, _ = database.ListPushDevices(8)
	if list[0].LastSeen.Before(before) {
		t.Fatalf("last_seen went backwards: %v -> %v", before, list[0].LastSeen)
	}

	// Owner-scoped delete leaves the device alone for the wrong owner.
	if err := database.DeletePushDevice(7, id); err != nil {
		t.Fatalf("delete wrong owner: %v", err)
	}
	if all, _ := database.ListAllPushDevices(); len(all) != 1 {
		t.Fatal("wrong-owner delete removed device")
	}
	if err := database.DeletePushDeviceByToken("tok-1"); err != nil {
		t.Fatalf("delete by token: %v", err)
	}
	if all, _ := database.ListAllPushDevices(); len(all) != 0 {
		t.Fatal("device not deleted")
	}
}

func TestGetAdminUserByID(t *testing.T) {
	database := setupTestDB(t)
	id, err := database.CreateAdminUser("byid", "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	u, err := database.GetAdminUserByID(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if u == nil || u.Username != "byid" || u.ID != id {
		t.Fatalf("unexpected user: %+v", u)
	}
}
