package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

const (
	personalOverdraftStoreVersion = 1
	personalOverdraftThreshold    = 95.0
	personalOverdraftStoreName    = "personal-overdraft.json"
)

type PersonalOverdraftStatus string

const (
	PersonalOverdraftNormal       PersonalOverdraftStatus = "normal"
	PersonalOverdraftPending      PersonalOverdraftStatus = "pending"
	PersonalOverdraftPassed       PersonalOverdraftStatus = "passed"
	PersonalOverdraftFailed       PersonalOverdraftStatus = "failed"
	PersonalOverdraftInconclusive PersonalOverdraftStatus = "inconclusive"
	PersonalOverdraftRecovered    PersonalOverdraftStatus = "recovered"
)

type PersonalOverdraftWindow string

const (
	PersonalOverdraftFiveHour PersonalOverdraftWindow = "5h"
	PersonalOverdraftSevenDay PersonalOverdraftWindow = "7d"
)

type PersonalOverdraftWindowState struct {
	Status       PersonalOverdraftStatus `json:"status"`
	CycleKey     string                  `json:"cycle_key,omitempty"`
	UsedPercent  float64                 `json:"used_percent"`
	ResetAt      time.Time               `json:"reset_at,omitempty"`
	EvidenceKind string                  `json:"evidence_kind,omitempty"`
	EvidenceAt   time.Time               `json:"evidence_at,omitempty"`
	ProbeClaimed bool                    `json:"probe_claimed"`
	ReasonCode   string                  `json:"reason_code,omitempty"`
	ChangedAt    time.Time               `json:"changed_at"`
}

type PersonalOverdraftAccountState struct {
	FiveHour PersonalOverdraftWindowState `json:"five_hour"`
	SevenDay PersonalOverdraftWindowState `json:"seven_day"`
}

type persistedPersonalOverdraftState struct {
	Version  int                                      `json:"version"`
	Accounts map[string]PersonalOverdraftAccountState `json:"accounts"`
}

type personalOverdraftProbe struct {
	AuthID string
	Cycle  map[PersonalOverdraftWindow]string
}

// PersonalOverdraftTracker is deliberately independent from the broad
// usage and model-test subsystems. It records only the two Codex quota
// windows and the request lifecycle needed for one real-business probe.
type PersonalOverdraftTracker struct {
	mu      sync.Mutex
	storeMu sync.Mutex

	enabled        bool
	storageHealthy bool
	storageErr     error
	store          string
	accounts       map[string]PersonalOverdraftAccountState
	probes         map[string]personalOverdraftProbe
	now            func() time.Time
	injector       *WeeklyOverdraftExperiment
}

func NewPersonalOverdraftTracker() *PersonalOverdraftTracker {
	tracker := &PersonalOverdraftTracker{
		accounts: make(map[string]PersonalOverdraftAccountState),
		probes:   make(map[string]personalOverdraftProbe),
		now:      time.Now,
	}
	tracker.injector = NewWeeklyOverdraftExperiment(func() bool { return true })
	return tracker
}

// Configure loads only sanitized state. An empty dataDir keeps the tracker
// usable in memory, which is useful for tests and fail-open host setups.
func (t *PersonalOverdraftTracker) Configure(enabled bool, dataDir string) {
	if t == nil {
		return
	}
	path := ""
	if strings.TrimSpace(dataDir) != "" {
		path = filepath.Join(dataDir, personalOverdraftStoreName)
	}
	accounts := make(map[string]PersonalOverdraftAccountState)
	storageHealthy := true
	var storageErr error
	if path != "" {
		if loaded, errLoad := loadPersonalOverdraftState(path); errLoad == nil {
			accounts = loaded
		} else if !errors.Is(errLoad, os.ErrNotExist) {
			storageHealthy = false
			storageErr = errLoad
		}
	}
	t.mu.Lock()
	t.enabled = enabled
	t.storageHealthy = storageHealthy
	t.storageErr = storageErr
	t.store = path
	t.accounts = accounts
	t.probes = make(map[string]personalOverdraftProbe)
	t.mu.Unlock()
}

func (t *PersonalOverdraftTracker) Enabled() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	enabled := t.enabled
	t.mu.Unlock()
	return enabled
}

func (t *PersonalOverdraftTracker) RequestInterceptionActive() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	active := t.enabled && t.storageHealthy
	t.mu.Unlock()
	return active
}

func (t *PersonalOverdraftTracker) RequestInterceptionAcceptsFormat(format string) bool {
	return strings.EqualFold(strings.TrimSpace(format), "codex")
}

func (t *PersonalOverdraftTracker) Snapshot(authID string) (PersonalOverdraftAccountState, bool) {
	if t == nil {
		return PersonalOverdraftAccountState{}, false
	}
	authID = strings.TrimSpace(authID)
	t.mu.Lock()
	state, ok := t.accounts[authID]
	t.mu.Unlock()
	return state, ok
}

func (t *PersonalOverdraftTracker) ObserveUsage(record cpaapi.UsageRecord) {
	if t == nil {
		return
	}
	t.mu.Lock()
	enabled := t.enabled
	t.mu.Unlock()
	if !enabled {
		return
	}
	authID := personalOverdraftAuthID(record)
	if authID == "" {
		return
	}
	now := t.currentTime()
	codex := parseCodexUsageHeaders(record.ResponseHeaders, now)
	body := strings.ToLower(strings.TrimSpace(record.Failure.Body))
	changed := false

	t.mu.Lock()
	account := t.accounts[authID]
	if codex != nil {
		changed = t.observeWindowLocked(authID, &account.FiveHour, PersonalOverdraftFiveHour, codex.FiveHour, now) || changed
		changed = t.observeWindowLocked(authID, &account.SevenDay, PersonalOverdraftSevenDay, codex.SevenDay, now) || changed
	}
	if record.Failed {
		if personalOverdraftExplicitQuota(record.Failure.StatusCode, body) {
			for _, window := range t.quotaEvidenceWindows(account, body) {
				if t.markFailedLocked(&account, window, now, "subscription_quota", "quota_exhausted") {
					changed = true
				}
			}
		}
	}
	if changed {
		account.FiveHour = sanitizePersonalOverdraftWindow(account.FiveHour)
		account.SevenDay = sanitizePersonalOverdraftWindow(account.SevenDay)
		t.accounts[authID] = account
	}
	t.mu.Unlock()
	if changed {
		_ = t.persist()
	}
}

func (t *PersonalOverdraftTracker) InterceptRequest(request cpaapi.RequestInterceptRequest) (cpaapi.RequestInterceptResponse, bool) {
	if t == nil || strings.TrimSpace(request.RequestID) == "" || !strings.EqualFold(strings.TrimSpace(request.ToFormat), "codex") {
		return cpaapi.RequestInterceptResponse{}, false
	}
	authID, _ := request.Metadata[selectedAuthMetadataKey].(string)
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return cpaapi.RequestInterceptResponse{}, false
	}
	t.mu.Lock()
	if !t.enabled || !t.storageHealthy {
		t.mu.Unlock()
		return cpaapi.RequestInterceptResponse{}, false
	}
	eligible := pendingWindows(t.accounts[authID])
	t.mu.Unlock()
	if len(eligible) == 0 {
		return cpaapi.RequestInterceptResponse{}, false
	}
	if t.injector == nil {
		return cpaapi.RequestInterceptResponse{}, false
	}
	modification, changed := t.injector.InterceptRequest(request)
	if !changed || len(modification.Body) == 0 {
		return cpaapi.RequestInterceptResponse{}, false
	}

	now := t.currentTime()
	t.mu.Lock()
	claimed, errClaim := t.claimProbeLocked(authID, request.RequestID, eligible, now)
	t.mu.Unlock()
	if errClaim != nil || len(claimed) == 0 {
		return cpaapi.RequestInterceptResponse{}, false
	}
	return modification, true
}

func (t *PersonalOverdraftTracker) claimProbeLocked(authID, requestID string, eligible []PersonalOverdraftWindow, now time.Time) (map[PersonalOverdraftWindow]string, error) {
	if !t.storageHealthy {
		return nil, t.storageFailureLocked()
	}
	if _, exists := t.probes[requestID]; exists {
		return nil, nil
	}
	if strings.TrimSpace(t.store) == "" {
		return t.claimProbeInMemoryLocked(authID, requestID, eligible, now), nil
	}
	t.storeMu.Lock()
	defer t.storeMu.Unlock()
	release, errLock := acquireUsageStoreLock(t.store)
	if errLock != nil {
		t.failStorageLocked(errLock)
		return nil, errLock
	}
	defer release()
	if loaded, errLoad := loadPersonalOverdraftState(t.store); errLoad == nil {
		t.accounts = mergePersonalOverdraftAccounts(t.accounts, loaded)
	} else if !os.IsNotExist(errLoad) {
		t.failStorageLocked(errLoad)
		return nil, errLoad
	}
	claimed := t.claimProbeInMemoryLocked(authID, requestID, eligible, now)
	if len(claimed) == 0 {
		return nil, nil
	}
	if errSave := savePersonalOverdraftState(t.store, t.accounts); errSave != nil {
		t.failStorageLocked(errSave)
		delete(t.probes, requestID)
		if loaded, errLoad := loadPersonalOverdraftState(t.store); errLoad == nil {
			t.accounts = loaded
		}
		return nil, errSave
	}
	return claimed, nil
}

func (t *PersonalOverdraftTracker) claimProbeInMemoryLocked(authID, requestID string, eligible []PersonalOverdraftWindow, now time.Time) map[PersonalOverdraftWindow]string {
	claimed := make(map[PersonalOverdraftWindow]string, len(eligible))
	account := t.accounts[authID]
	for _, window := range eligible {
		state := personalOverdraftWindowState(account, window)
		if state.Status != PersonalOverdraftPending || state.ProbeClaimed || state.CycleKey == "" {
			continue
		}
		claimed[window] = state.CycleKey
		state.ProbeClaimed = true
		state.ChangedAt = now
		setPersonalOverdraftWindowState(&account, window, state)
	}
	if len(claimed) > 0 {
		t.accounts[authID] = account
		t.probes[requestID] = personalOverdraftProbe{AuthID: authID, Cycle: claimed}
	}
	return claimed
}

func (t *PersonalOverdraftTracker) Complete(completion cpaapi.RequestCompletion) {
	if t == nil || strings.TrimSpace(completion.RequestID) == "" {
		return
	}
	now := t.currentTime()
	t.mu.Lock()
	probe, ok := t.probes[completion.RequestID]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.probes, completion.RequestID)
	account := t.accounts[probe.AuthID]
	success := personalOverdraftCompletionSuccess(completion)
	changed := false
	for window, cycle := range probe.Cycle {
		state := personalOverdraftWindowState(account, window)
		if state.CycleKey != cycle || state.Status == PersonalOverdraftFailed {
			continue
		}
		if success {
			state.Status = PersonalOverdraftPassed
			state.EvidenceKind, state.ReasonCode = "real_business_success", "probe_passed"
		} else {
			state.Status = PersonalOverdraftInconclusive
			state.EvidenceKind, state.ReasonCode = "request_completion", "inconclusive_completion"
		}
		state.EvidenceAt, state.ChangedAt = now, now
		setPersonalOverdraftWindowState(&account, window, state)
		changed = true
	}
	if changed {
		t.accounts[probe.AuthID] = account
		_ = t.persistLocked()
	}
	t.mu.Unlock()
}

func (t *PersonalOverdraftTracker) observeWindowLocked(authID string, state *PersonalOverdraftWindowState, window PersonalOverdraftWindow, observed *UsageWindowSnapshot, now time.Time) bool {
	if observed == nil || state == nil {
		return false
	}
	changed := false
	state.UsedPercent = observed.UsedPercent
	if observed.ResetAt != nil {
		state.ResetAt = observed.ResetAt.UTC()
	}
	if state.Status != "" && state.Status != PersonalOverdraftNormal && state.Status != PersonalOverdraftRecovered &&
		(state.ResetAt.IsZero() || state.ResetAt.After(now)) && observed.UsedPercent >= personalOverdraftThreshold {
		return true
	}
	if observed.UsedPercent < personalOverdraftThreshold || !state.ResetAt.IsZero() && !state.ResetAt.After(now) {
		if state.Status == PersonalOverdraftRecovered {
			state.Status = PersonalOverdraftNormal
			state.ProbeClaimed = false
			state.ChangedAt = now
			return true
		}
		if state.CycleKey != "" && state.Status != PersonalOverdraftNormal {
			state.Status = PersonalOverdraftRecovered
			state.CycleKey = fmt.Sprintf("%s-recovered-%d", window, now.UnixNano())
			state.ProbeClaimed = false
			state.EvidenceKind, state.EvidenceAt, state.ReasonCode, state.ChangedAt = "quota_reset", now, "quota_recovered", now
			return true
		}
		if state.Status == "" {
			state.Status = PersonalOverdraftNormal
			state.ChangedAt = now
			return true
		}
		return false
	}
	if observed.UsedPercent >= personalOverdraftThreshold && (state.Status == "" || state.Status == PersonalOverdraftNormal || state.Status == PersonalOverdraftRecovered) {
		state.Status = PersonalOverdraftPending
		state.CycleKey = fmt.Sprintf("%s-%d", window, now.UnixNano())
		state.ProbeClaimed = false
		state.EvidenceKind, state.EvidenceAt, state.ReasonCode, state.ChangedAt = "quota_header", now, "quota_near_limit", now
		return true
	}
	return changed
}

func (t *PersonalOverdraftTracker) quotaEvidenceWindows(account PersonalOverdraftAccountState, body string) []PersonalOverdraftWindow {
	if strings.Contains(body, "weekly") || strings.Contains(body, "7-day") || strings.Contains(body, "7 day") || strings.Contains(body, "seven_day") {
		return []PersonalOverdraftWindow{PersonalOverdraftSevenDay}
	}
	if strings.Contains(body, "5-hour") || strings.Contains(body, "5 hour") || strings.Contains(body, "five_hour") || strings.Contains(body, "5h") {
		return []PersonalOverdraftWindow{PersonalOverdraftFiveHour}
	}
	var windows []PersonalOverdraftWindow
	if account.FiveHour.Status == PersonalOverdraftPending && account.FiveHour.UsedPercent >= 100 {
		windows = append(windows, PersonalOverdraftFiveHour)
	}
	if account.SevenDay.Status == PersonalOverdraftPending && account.SevenDay.UsedPercent >= 100 {
		windows = append(windows, PersonalOverdraftSevenDay)
	}
	if len(windows) == 1 {
		return windows
	}
	return nil
}

func (t *PersonalOverdraftTracker) markFailedLocked(account *PersonalOverdraftAccountState, window PersonalOverdraftWindow, now time.Time, evidence, reason string) bool {
	state := personalOverdraftWindowState(*account, window)
	if state.CycleKey == "" || state.Status == PersonalOverdraftFailed {
		return false
	}
	state.Status = PersonalOverdraftFailed
	state.ProbeClaimed = true
	state.EvidenceKind, state.EvidenceAt, state.ReasonCode, state.ChangedAt = evidence, now, reason, now
	setPersonalOverdraftWindowState(account, window, state)
	return true
}

func (t *PersonalOverdraftTracker) persist() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.persistLocked()
}

func (t *PersonalOverdraftTracker) persistLocked() error {
	if !t.storageHealthy {
		return t.storageFailureLocked()
	}
	if strings.TrimSpace(t.store) == "" {
		return nil
	}
	state := persistedPersonalOverdraftState{Version: personalOverdraftStoreVersion, Accounts: clonePersonalOverdraftAccounts(t.accounts)}
	t.storeMu.Lock()
	defer t.storeMu.Unlock()
	release, errLock := acquireUsageStoreLock(t.store)
	if errLock != nil {
		t.failStorageLocked(errLock)
		return errLock
	}
	defer release()
	if loaded, errLoad := loadPersonalOverdraftState(t.store); errLoad == nil {
		state.Accounts = mergePersonalOverdraftAccounts(state.Accounts, loaded)
		t.accounts = clonePersonalOverdraftAccounts(state.Accounts)
	} else if !os.IsNotExist(errLoad) {
		t.failStorageLocked(errLoad)
		return errLoad
	}
	if errSave := savePersonalOverdraftState(t.store, state.Accounts); errSave != nil {
		t.failStorageLocked(errSave)
		return errSave
	}
	return nil
}

func (t *PersonalOverdraftTracker) failStorageLocked(err error) {
	if err == nil {
		return
	}
	t.storageHealthy = false
	t.storageErr = err
}

func (t *PersonalOverdraftTracker) storageFailureLocked() error {
	if t.storageErr != nil {
		return t.storageErr
	}
	return fmt.Errorf("personal overdraft storage is unavailable")
}

func savePersonalOverdraftState(path string, accounts map[string]PersonalOverdraftAccountState) error {
	state := persistedPersonalOverdraftState{Version: personalOverdraftStoreVersion, Accounts: sanitizePersonalOverdraftAccounts(accounts)}
	if errSave := savePrivateJSON(path, state); errSave != nil {
		return errSave
	}
	// The primary file is the claim authority. A backup failure must not make a
	// successfully persisted claim look reusable to the caller.
	_ = savePrivateJSON(path+".bak", state)
	return nil
}

func loadPersonalOverdraftState(path string) (map[string]PersonalOverdraftAccountState, error) {
	found := false
	var failures []error
	for _, candidate := range []string{path, path + ".bak"} {
		raw, errRead := os.ReadFile(candidate)
		if errRead != nil {
			if !errors.Is(errRead, os.ErrNotExist) {
				found = true
				failures = append(failures, fmt.Errorf("read %s: %w", filepath.Base(candidate), errRead))
			}
			continue
		}
		found = true
		var state persistedPersonalOverdraftState
		if errDecode := json.Unmarshal(raw, &state); errDecode != nil {
			failures = append(failures, fmt.Errorf("decode %s: %w", filepath.Base(candidate), errDecode))
			continue
		}
		if state.Version != personalOverdraftStoreVersion {
			failures = append(failures, fmt.Errorf("decode %s: unsupported version %d", filepath.Base(candidate), state.Version))
			continue
		}
		return sanitizePersonalOverdraftAccounts(state.Accounts), nil
	}
	if found {
		return nil, fmt.Errorf("load personal overdraft state: %w", errors.Join(failures...))
	}
	return nil, os.ErrNotExist
}

func (t *PersonalOverdraftTracker) currentTime() time.Time {
	if t != nil && t.now != nil {
		return t.now().UTC()
	}
	return time.Now().UTC()
}

func personalOverdraftAuthID(record cpaapi.UsageRecord) string {
	if id := strings.TrimSpace(record.AuthID); id != "" {
		return id
	}
	return strings.TrimSpace(record.AuthIndex)
}

func personalOverdraftExplicitQuota(status int, body string) bool {
	if status != http.StatusTooManyRequests && status != http.StatusPaymentRequired {
		return false
	}
	return strings.Contains(body, "usage_limit_reached") || strings.Contains(body, "usage limit has been reached") ||
		strings.Contains(body, "quota exhausted") || strings.Contains(body, "weekly limit reached")
}

func personalOverdraftCompletionSuccess(completion cpaapi.RequestCompletion) bool {
	outcome := strings.ToLower(strings.TrimSpace(completion.Outcome))
	return (outcome == "success" || outcome == "succeeded" || outcome == "completed") && completion.StatusCode >= 200 && completion.StatusCode < 300 ||
		completion.StatusCode >= 200 && completion.StatusCode < 300 && outcome != "failed" && outcome != "error" && outcome != "canceled"
}

func pendingWindows(account PersonalOverdraftAccountState) []PersonalOverdraftWindow {
	var windows []PersonalOverdraftWindow
	if account.FiveHour.Status == PersonalOverdraftPending && !account.FiveHour.ProbeClaimed {
		windows = append(windows, PersonalOverdraftFiveHour)
	}
	if account.SevenDay.Status == PersonalOverdraftPending && !account.SevenDay.ProbeClaimed {
		windows = append(windows, PersonalOverdraftSevenDay)
	}
	return windows
}

func personalOverdraftWindowState(account PersonalOverdraftAccountState, window PersonalOverdraftWindow) PersonalOverdraftWindowState {
	if window == PersonalOverdraftSevenDay {
		return account.SevenDay
	}
	return account.FiveHour
}

func setPersonalOverdraftWindowState(account *PersonalOverdraftAccountState, window PersonalOverdraftWindow, state PersonalOverdraftWindowState) {
	if window == PersonalOverdraftSevenDay {
		account.SevenDay = state
	} else {
		account.FiveHour = state
	}
}

func sanitizePersonalOverdraftAccounts(accounts map[string]PersonalOverdraftAccountState) map[string]PersonalOverdraftAccountState {
	result := make(map[string]PersonalOverdraftAccountState, len(accounts))
	for authID, account := range accounts {
		authID = strings.TrimSpace(authID)
		if authID == "" || len(authID) > 4096 {
			continue
		}
		account.FiveHour = sanitizePersonalOverdraftWindow(account.FiveHour)
		account.SevenDay = sanitizePersonalOverdraftWindow(account.SevenDay)
		result[authID] = account
	}
	return result
}

func sanitizePersonalOverdraftWindow(state PersonalOverdraftWindowState) PersonalOverdraftWindowState {
	switch state.Status {
	case PersonalOverdraftNormal, PersonalOverdraftPending, PersonalOverdraftPassed, PersonalOverdraftFailed, PersonalOverdraftInconclusive, PersonalOverdraftRecovered:
	default:
		state = PersonalOverdraftWindowState{Status: PersonalOverdraftNormal}
	}
	if state.UsedPercent < 0 || state.UsedPercent > 10_000 {
		state.UsedPercent = 0
	}
	state.CycleKey = strings.TrimSpace(state.CycleKey)
	state.ReasonCode = strings.TrimSpace(state.ReasonCode)
	state.EvidenceKind = strings.TrimSpace(state.EvidenceKind)
	return state
}

func clonePersonalOverdraftAccounts(accounts map[string]PersonalOverdraftAccountState) map[string]PersonalOverdraftAccountState {
	result := make(map[string]PersonalOverdraftAccountState, len(accounts))
	for authID, account := range accounts {
		result[authID] = account
	}
	return result
}

func mergePersonalOverdraftAccounts(current, stored map[string]PersonalOverdraftAccountState) map[string]PersonalOverdraftAccountState {
	merged := clonePersonalOverdraftAccounts(stored)
	for authID, account := range current {
		previous := merged[authID]
		previous.FiveHour = mergePersonalOverdraftWindow(account.FiveHour, previous.FiveHour)
		previous.SevenDay = mergePersonalOverdraftWindow(account.SevenDay, previous.SevenDay)
		merged[authID] = previous
	}
	return sanitizePersonalOverdraftAccounts(merged)
}

func mergePersonalOverdraftWindow(current, stored PersonalOverdraftWindowState) PersonalOverdraftWindowState {
	if stored.CycleKey == "" {
		return current
	}
	if current.CycleKey == "" {
		return stored
	}
	if current.CycleKey != stored.CycleKey {
		if !current.ChangedAt.Before(stored.ChangedAt) {
			return current
		}
		return stored
	}
	if current.Status == PersonalOverdraftFailed {
		return current
	}
	if stored.Status == PersonalOverdraftFailed {
		return stored
	}
	selected := stored
	if current.ChangedAt.After(stored.ChangedAt) ||
		current.ChangedAt.Equal(stored.ChangedAt) && personalOverdraftStatusRank(current.Status) > personalOverdraftStatusRank(stored.Status) {
		selected = current
	}
	selected.ProbeClaimed = current.ProbeClaimed || stored.ProbeClaimed
	return selected
}

func personalOverdraftStatusRank(status PersonalOverdraftStatus) int {
	switch status {
	case PersonalOverdraftRecovered:
		return 4
	case PersonalOverdraftPassed:
		return 3
	case PersonalOverdraftInconclusive:
		return 2
	case PersonalOverdraftPending:
		return 1
	default:
		return 0
	}
}
