package manager

import (
	"context"
	"strings"
	"sync"
	"time"
)

const personalGatewayQuotaRefreshInterval = 10 * time.Minute

type personalGatewayQuotaRefreshHandler func(context.Context)

type personalGatewayQuotaRefreshWorker struct {
	mu              sync.Mutex
	wait            sync.WaitGroup
	backgroundOwner BackgroundWorkOwner
	handler         personalGatewayQuotaRefreshHandler
	interval        time.Duration
	cancel          context.CancelFunc
	started         bool
	closed          bool
}

func newPersonalGatewayQuotaRefreshWorker() *personalGatewayQuotaRefreshWorker {
	return &personalGatewayQuotaRefreshWorker{interval: personalGatewayQuotaRefreshInterval}
}

func (w *personalGatewayQuotaRefreshWorker) SetBackgroundWorkOwner(owner BackgroundWorkOwner) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.backgroundOwner = owner
	w.mu.Unlock()
}

func (w *personalGatewayQuotaRefreshWorker) SetHandler(handler personalGatewayQuotaRefreshHandler) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.handler = handler
	w.mu.Unlock()
}

func (w *personalGatewayQuotaRefreshWorker) Start() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true
	w.wait.Add(1)
	w.mu.Unlock()
	go w.run(ctx)
}

func (w *personalGatewayQuotaRefreshWorker) Shutdown() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.wait.Wait()
}

func (w *personalGatewayQuotaRefreshWorker) run(ctx context.Context) {
	defer w.wait.Done()
	w.mu.Lock()
	interval := w.interval
	w.mu.Unlock()
	if interval <= 0 {
		interval = personalGatewayQuotaRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refresh(ctx)
		}
	}
}

func (w *personalGatewayQuotaRefreshWorker) refresh(ctx context.Context) {
	if w == nil || ctx.Err() != nil {
		return
	}
	w.mu.Lock()
	owner, handler := w.backgroundOwner, w.handler
	w.mu.Unlock()
	if handler == nil || !backgroundWorkAllowed(owner) {
		return
	}
	ownedCtx, cancelOwnership := contextWithBackgroundOwnership(ctx, owner)
	handler(ownedCtx)
	cancelOwnership()
}

func (a *App) refreshPersonalGatewayQuotas(ctx context.Context) {
	if a == nil || ctx.Err() != nil {
		return
	}
	accounts, errList := a.accounts.baseAccounts(ctx)
	if errList != nil {
		return
	}
	for _, account := range personalGatewayQuotaAccounts(a.configSnapshot().PersonalGateway, accounts) {
		if ctx.Err() != nil {
			return
		}
		_ = a.refreshAccountQuotaMetadataDirect(ctx, account)
	}
}

func personalGatewayQuotaAccounts(config PersonalGatewayConfig, accounts []Account) []Account {
	byAuthID := make(map[string]Account, len(accounts))
	ambiguous := make(map[string]struct{})
	for _, account := range accounts {
		authID := strings.TrimSpace(account.AuthID)
		if authID == "" || strings.TrimSpace(account.ID) == "" || !quotaMetadataBootstrapEligible(account) {
			continue
		}
		if _, duplicate := byAuthID[authID]; duplicate {
			delete(byAuthID, authID)
			ambiguous[authID] = struct{}{}
			continue
		}
		if _, duplicate := ambiguous[authID]; !duplicate {
			byAuthID[authID] = quotaMetadataBootstrapAccount(account)
		}
	}

	selected := make([]Account, 0, 2)
	seenIndexes := make(map[string]struct{}, 2)
	for _, authID := range []string{strings.TrimSpace(config.AccountAID), strings.TrimSpace(config.AccountBID)} {
		account, exists := byAuthID[authID]
		if !exists {
			continue
		}
		if _, duplicate := seenIndexes[account.ID]; duplicate {
			continue
		}
		seenIndexes[account.ID] = struct{}{}
		selected = append(selected, account)
	}
	return selected
}
