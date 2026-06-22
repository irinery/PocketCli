package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInventoryNormalizesHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := filepath.Join(home, ".local", "share", "pocketcli")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	inventory := `{"schema_version":1,"hosts":[{"id":"host-a","hostname":"a","tailscale_ip":"100.0.0.1","source":["tailscale"],"online":true},{"id":"host-b","hostname":"b","source":["saved"],"online":true},{"id":"host-c","hostname":"c","source":["seed"],"online":false}]}`
	if err := os.WriteFile(filepath.Join(dataDir, "inventory.json"), []byte(inventory), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	result, err := LoadInventory()
	if err != nil {
		t.Fatalf("LoadInventory returned error: %v", err)
	}
	if len(result.Hosts) != 3 || result.OnlineCount != 2 {
		t.Fatalf("unexpected inventory result: %#v", result)
	}
}

func TestCreatePlanRejectsEmptySelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := filepath.Join(home, ".local", "share", "pocketcli")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "inventory.json"), []byte(`{"hosts":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	_, err := CreatePlan("tag:db", []string{"echo", "ok"}, 4)
	if err != ErrEmptySelection {
		t.Fatalf("expected ErrEmptySelection, got %v", err)
	}
}
