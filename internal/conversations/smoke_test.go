package conversations

import (
	"os"
	"testing"
)

// TestSmokeRealData is a manual probe against the developer's own agent data.
// It is skipped unless DECLAW_SMOKE=1 so CI never depends on local state.
func TestSmokeRealData(t *testing.T) {
	if os.Getenv("DECLAW_SMOKE") != "1" {
		t.Skip("set DECLAW_SMOKE=1 to probe local agent data")
	}
	registry := DefaultRegistry()
	list, err := registry.List()
	t.Logf("err=%v total=%d", err, len(list))
	byHarness := map[string]int{}
	for _, c := range list {
		byHarness[string(c.Harness)]++
	}
	t.Logf("by harness: %v", byHarness)
	shown := 0
	for _, c := range list {
		if shown >= 8 {
			break
		}
		shown++
		t.Logf("[%s] %s | %s | %s | msgs=%d compacted=%v", c.Harness, c.UpdatedAt.Format("2006-01-02 15:04"), c.Title, c.Path, c.MessageCount, c.Compacted)
	}
	for _, c := range list {
		if c.MessageCount > 4 {
			tr, err := registry.Load(c)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			t.Logf("loaded %s msgs=%d compacted=%v", c.Harness, len(tr.Messages), tr.Compacted)
			p := HandoffPrompt(tr, "claude")
			t.Logf("prompt chars=%d head=%.300s", len(p), p)
			break
		}
	}
}
