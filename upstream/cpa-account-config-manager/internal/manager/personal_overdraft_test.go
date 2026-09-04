package manager

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

func personalOverdraftHeaders(five, seven float64, now time.Time) http.Header {
	return http.Header{
		"X-Codex-Secondary-Used-Percent":        {formatPersonalPercent(five)},
		"X-Codex-Secondary-Window-Minutes":      {"300"},
		"X-Codex-Secondary-Reset-After-Seconds": {"18000"},
		"X-Codex-Primary-Used-Percent":          {formatPersonalPercent(seven)},
		"X-Codex-Primary-Window-Minutes":        {"10080"},
		"X-Codex-Primary-Reset-At":              {formatPersonalUnix(now.Add(7 * 24 * time.Hour))},
	}
}

func formatPersonalPercent(value float64) string { return fmt.Sprintf("%g", value) }
func formatPersonalUnix(value time.Time) string  { return fmt.Sprintf("%d", value.Unix()) }

func personalOverdraftRequest(id, authID string) cpaapi.RequestInterceptRequest {
	return cpaapi.RequestInterceptRequest{
		RequestID: id, ToFormat: "codex", Metadata: map[string]any{selectedAuthMetadataKey: authID},
		Body: []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"continue"}]}`),
	}
}

func TestPersonalOverdraftConcurrentClaimOnlyInjectsOnce(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, t.TempDir())
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-a", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})

	var wg sync.WaitGroup
	var mu sync.Mutex
	changed := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			response, ok := tracker.InterceptRequest(personalOverdraftRequest(fmt.Sprintf("request-%d", i), "auth-a"))
			if ok && len(response.Body) > 0 {
				mu.Lock()
				changed++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if changed != 1 {
		t.Fatalf("concurrent claim count = %d, want 1", changed)
	}
}

func TestPersonalOverdraftOverlappingInstancesClaimOnlyOnce(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	first := NewPersonalOverdraftTracker()
	first.now = func() time.Time { return now }
	first.Configure(true, dataDir)
	first.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-overlap", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	second := NewPersonalOverdraftTracker()
	second.now = func() time.Time { return now }
	second.Configure(true, dataDir)

	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for index, tracker := range []*PersonalOverdraftTracker{first, second} {
		wg.Add(1)
		go func(index int, tracker *PersonalOverdraftTracker) {
			defer wg.Done()
			_, changed := tracker.InterceptRequest(personalOverdraftRequest(fmt.Sprintf("overlap-%d", index), "auth-overlap"))
			results <- changed
		}(index, tracker)
	}
	wg.Wait()
	close(results)
	claimed := 0
	for changed := range results {
		if changed {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("overlapping instance claim count = %d, want 1", claimed)
	}
}

func TestPersonalOverdraftClaimsBothWindowsAndPassesTogether(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, t.TempDir())
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-b", ResponseHeaders: personalOverdraftHeaders(100, 100, now)})
	request := personalOverdraftRequest("request-b", "auth-b")
	if _, ok := tracker.InterceptRequest(request); !ok {
		t.Fatal("both pending windows were not injected")
	}
	state, ok := tracker.Snapshot("auth-b")
	if !ok || !state.FiveHour.ProbeClaimed || !state.SevenDay.ProbeClaimed {
		t.Fatalf("claimed state = %#v", state)
	}
	tracker.Complete(cpaapi.RequestCompletion{RequestID: "request-b", Outcome: "succeeded", StatusCode: http.StatusOK})
	state, _ = tracker.Snapshot("auth-b")
	if state.FiveHour.Status != PersonalOverdraftPassed || state.SevenDay.Status != PersonalOverdraftPassed {
		t.Fatalf("passed state = %#v", state)
	}
}

func TestPersonalOverdraftFailedIsTerminalAndStaleCycleCannotPass(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, t.TempDir())
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-c", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	first := personalOverdraftRequest("request-c-old", "auth-c")
	if _, ok := tracker.InterceptRequest(first); !ok {
		t.Fatal("first probe was not claimed")
	}
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-c", Failed: true, Failure: cpaapi.UsageFailure{StatusCode: http.StatusTooManyRequests, Body: `{"error":"usage_limit_reached"}`}})
	state, _ := tracker.Snapshot("auth-c")
	if state.FiveHour.Status != PersonalOverdraftFailed {
		t.Fatalf("failed state = %#v", state.FiveHour)
	}
	oldCycle := state.FiveHour.CycleKey
	now = now.Add(6 * time.Hour)
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-c", ResponseHeaders: personalOverdraftHeaders(0, 10, now)})
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-c", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	state, _ = tracker.Snapshot("auth-c")
	if state.FiveHour.CycleKey == oldCycle || state.FiveHour.Status != PersonalOverdraftPending {
		t.Fatalf("new cycle = %#v", state.FiveHour)
	}
	tracker.Complete(cpaapi.RequestCompletion{RequestID: "request-c-old", Outcome: "succeeded", StatusCode: http.StatusOK})
	state, _ = tracker.Snapshot("auth-c")
	if state.FiveHour.Status != PersonalOverdraftPending {
		t.Fatalf("stale completion changed new cycle = %#v", state.FiveHour)
	}
}

func TestPersonalOverdraftFailedCompletionCannotBeOverwritten(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, t.TempDir())
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-terminal", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	if _, ok := tracker.InterceptRequest(personalOverdraftRequest("request-terminal", "auth-terminal")); !ok {
		t.Fatal("probe was not claimed")
	}
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-terminal", Failed: true, Failure: cpaapi.UsageFailure{StatusCode: http.StatusTooManyRequests, Body: `{"error":"usage_limit_reached"}`}})
	tracker.Complete(cpaapi.RequestCompletion{RequestID: "request-terminal", Outcome: "succeeded", StatusCode: http.StatusOK})
	state, _ := tracker.Snapshot("auth-terminal")
	if state.FiveHour.Status != PersonalOverdraftFailed {
		t.Fatalf("late success overwrote failed state = %#v", state.FiveHour)
	}
}

func TestPersonalOverdraftUnknown429IsInconclusive(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, t.TempDir())
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-d", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	if _, ok := tracker.InterceptRequest(personalOverdraftRequest("request-d", "auth-d")); !ok {
		t.Fatal("probe was not claimed")
	}
	tracker.Complete(cpaapi.RequestCompletion{RequestID: "request-d", Outcome: "failed", StatusCode: http.StatusTooManyRequests})
	state, _ := tracker.Snapshot("auth-d")
	if state.FiveHour.Status != PersonalOverdraftInconclusive {
		t.Fatalf("unknown 429 state = %#v", state.FiveHour)
	}
}

func TestPersonalOverdraftClaimPersistsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	first := NewPersonalOverdraftTracker()
	first.now = func() time.Time { return now }
	first.Configure(true, dataDir)
	first.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-e", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	if _, ok := first.InterceptRequest(personalOverdraftRequest("request-e", "auth-e")); !ok {
		t.Fatal("probe was not claimed")
	}
	second := NewPersonalOverdraftTracker()
	second.now = func() time.Time { return now }
	second.Configure(true, dataDir)
	state, ok := second.Snapshot("auth-e")
	if !ok || !state.FiveHour.ProbeClaimed {
		t.Fatalf("reloaded claim = %#v", state)
	}
	if _, ok := second.InterceptRequest(personalOverdraftRequest("request-e-2", "auth-e")); ok {
		t.Fatal("persisted claim was reused")
	}
}

func TestPersonalOverdraftCorruptStateFailsClosed(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()
	for _, name := range []string{personalOverdraftStoreName, personalOverdraftStoreName + ".bak"} {
		if errWrite := os.WriteFile(filepath.Join(dataDir, name), []byte("not-json"), 0o600); errWrite != nil {
			t.Fatalf("write corrupt state: %v", errWrite)
		}
	}
	tracker := NewPersonalOverdraftTracker()
	tracker.now = func() time.Time { return now }
	tracker.Configure(true, dataDir)
	tracker.ObserveUsage(cpaapi.UsageRecord{AuthID: "auth-corrupt", ResponseHeaders: personalOverdraftHeaders(100, 10, now)})
	if tracker.RequestInterceptionActive() {
		t.Fatal("corrupt storage left request interception active")
	}
	if _, changed := tracker.InterceptRequest(personalOverdraftRequest("request-corrupt", "auth-corrupt")); changed {
		t.Fatal("corrupt storage allowed a probe claim")
	}
}

func TestPersonalOverdraftRequiresLifecycleSchema(t *testing.T) {
	app := NewApp(nil, nil)
	defer app.Close()
	app.ConfigureHost([]byte("data_dir: "+t.TempDir()+"\npersonal_gateway:\n  overdraft_enabled: true\n"), cpaapi.LegacySchemaVersion)
	if app.personalOverdraft.Enabled() || app.RequestCompletionActive() {
		t.Fatal("legacy host enabled lifecycle-dependent overdraft")
	}
}
