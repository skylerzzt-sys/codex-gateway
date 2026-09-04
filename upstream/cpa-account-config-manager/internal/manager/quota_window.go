package manager

import "strings"

const (
	QuotaWindowFiveHour         = "five_hour"
	QuotaWindowSevenDay         = "seven_day"
	QuotaWindowMultiple         = "multiple"
	QuotaWindowFiveHourFallback = "five_hour_fallback"

	ModelProbeKindModel      = "model"
	ModelProbeKindCredential = "credential"

	maxTrackedAccounts = 10_000
)

func normalizeQuotaWindow(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case QuotaWindowFiveHour, QuotaWindowSevenDay, QuotaWindowMultiple, QuotaWindowFiveHourFallback:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}
