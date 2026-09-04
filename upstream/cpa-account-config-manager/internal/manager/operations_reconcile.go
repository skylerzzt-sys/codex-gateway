package manager

import (
	"fmt"
	"time"
)

func (a *App) reconcileOperationSources() {
	if a == nil || a.operations == nil {
		return
	}
	update := a.updates.Snapshot()
	if !update.CheckedAt.IsZero() {
		a.operations.Upsert(operationTimestampEvent("update-check", update.CheckedAt), operationFromUpdateCheck(update))
	}
}

func operationFromUpdateCheck(snapshot UpdateSnapshot) OperationEntry {
	status := OperationStatusSucceeded
	reason := "check_completed"
	if snapshot.Error != "" {
		status = OperationStatusFailed
		reason = "check_failed"
	}
	return OperationEntry{
		Category: OperationCategoryUpdate, Action: OperationActionUpdateCheck, Status: status,
		Source: OperationSourcePluginStore, Scope: OperationScopeSystem,
		StartedAt: snapshot.CheckedAt, FinishedAt: snapshot.CheckedAt, ReasonCode: reason,
	}
}

func operationTimestampEvent(prefix string, value time.Time) string {
	return fmt.Sprintf("%s:%s", prefix, value.UTC().Format(time.RFC3339Nano))
}
