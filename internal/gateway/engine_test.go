package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestOfflineRuleAndStoreForward(t *testing.T) {
	e := NewEngine(Snapshot{})
	a, err := e.AddAdapter("模拟产线", "simulator", "line://1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.AddPoint(a.ID, "line.temperature", "40001", "℃", .1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = e.AddRule("高温", "line.temperature", "gt", 80, "alarm")
	point, events, err := e.Ingest("line.temperature", 850, "good")
	if err != nil || point.Value != 85 || len(events) != 1 {
		t.Fatalf("bad ingest %#v %#v %v", point, events, err)
	}
	if _, err = e.PendingBatch(10); err == nil {
		t.Fatal("offline gateway must not drain outbox")
	}
	e.SetCloudOnline(true)
	batch, err := e.PendingBatch(10)
	if err != nil || len(batch) < 2 {
		t.Fatalf("want telemetry and event: %v %v", batch, err)
	}
	ids := []string{}
	for _, x := range batch {
		ids = append(ids, x.ID)
	}
	e.Ack(ids)
	if e.Dashboard()["metrics"].(map[string]any)["pending"].(int) != 0 {
		t.Fatal("outbox should be empty")
	}
}
func TestConfigChecksumAndRevision(t *testing.T) {
	e := NewEngine(Snapshot{})
	body := "{\"poll\":5}"
	sum := sha256.Sum256([]byte(body))
	checksum := hex.EncodeToString(sum[:])
	if _, err := e.ApplyConfig(1, body, checksum, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ApplyConfig(1, body, checksum, "admin"); err == nil {
		t.Fatal("old revision accepted")
	}
	if _, err := e.ApplyConfig(2, body, "bad", "admin"); err == nil {
		t.Fatal("bad checksum accepted")
	}
}
