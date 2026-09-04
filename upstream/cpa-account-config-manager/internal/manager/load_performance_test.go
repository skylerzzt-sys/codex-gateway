package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

type delayedDetailAuthHost struct {
	*fakeAuthHost
	delay time.Duration
}
type blockingDetailAuthHost struct {
	*fakeAuthHost
	mu        sync.Mutex
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
}

func (h *blockingDetailAuthHost) GetAuth(ctx context.Context, id string) (cpaapi.HostAuthGetResponse, error) {
	h.mu.Lock()
	h.active++
	if h.active > h.maxActive {
		h.maxActive = h.active
	}
	h.mu.Unlock()
	h.started <- struct{}{}
	select {
	case <-ctx.Done():
	case <-h.release:
	}
	h.mu.Lock()
	h.active--
	h.mu.Unlock()
	if errContext := ctx.Err(); errContext != nil {
		return cpaapi.HostAuthGetResponse{}, errContext
	}
	return h.fakeAuthHost.GetAuth(ctx, id)
}

func (h *delayedDetailAuthHost) GetAuth(ctx context.Context, id string) (cpaapi.HostAuthGetResponse, error) {
	timer := time.NewTimer(h.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return cpaapi.HostAuthGetResponse{}, ctx.Err()
	case <-timer.C:
		return h.fakeAuthHost.GetAuth(ctx, id)
	}
}

func BenchmarkAccountListDetailLoading(b *testing.B) {
	entries, details := accountDetailFixtures(50)
	host := &delayedDetailAuthHost{
		fakeAuthHost: &fakeAuthHost{entries: entries, details: details},
		delay:        time.Millisecond,
	}
	accounts := NewAccountService(host)
	b.ResetTimer()
	for range b.N {
		response, errList := accounts.List(context.Background(), ListQuery{Page: 1, PageSize: 50})
		if errList != nil || len(response.Accounts) != len(entries) {
			b.Fatalf("list = %d accounts, %v", len(response.Accounts), errList)
		}
	}
}

func TestAccountDetailLoadingUsesBoundedConcurrency(t *testing.T) {
	entries, details := accountDetailFixtures(accountDetailWorkers + 4)
	host := &blockingDetailAuthHost{
		fakeAuthHost: &fakeAuthHost{entries: entries, details: details},
		started:      make(chan struct{}, len(entries)),
		release:      make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		response, errList := NewAccountService(host).List(context.Background(), ListQuery{Page: 1, PageSize: len(entries)})
		if errList == nil && len(response.Accounts) != len(entries) {
			errList = fmt.Errorf("list returned %d accounts, want %d", len(response.Accounts), len(entries))
		}
		done <- errList
	}()
	for range accountDetailWorkers {
		select {
		case <-host.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent account detail reads")
		}
	}
	host.mu.Lock()
	active, maxActive := host.active, host.maxActive
	host.mu.Unlock()
	if active < 2 {
		t.Fatalf("active detail reads = %d, want at least 2", active)
	}
	if maxActive > accountDetailWorkers {
		t.Fatalf("maximum detail reads = %d, want at most %d", maxActive, accountDetailWorkers)
	}
	close(host.release)
	if errList := <-done; errList != nil {
		t.Fatal(errList)
	}
}

func accountDetailFixtures(count int) ([]cpaapi.HostAuthFileEntry, map[string]cpaapi.HostAuthGetResponse) {
	entries := make([]cpaapi.HostAuthFileEntry, count)
	details := make(map[string]cpaapi.HostAuthGetResponse, len(entries))
	for index := range entries {
		id := fmt.Sprintf("auth-%03d", index)
		path := fmt.Sprintf("/auth/%s.json", id)
		entries[index] = cpaapi.HostAuthFileEntry{
			AuthIndex: id, Name: id + ".json", Provider: "codex", Type: "codex", Source: "file", Path: path,
		}
		details[id] = cpaapi.HostAuthGetResponse{
			AuthIndex: id, Name: id + ".json", Path: path,
			JSON: json.RawMessage(`{"type":"codex","priority":1}`),
		}
	}
	return entries, details
}
