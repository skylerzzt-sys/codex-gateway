package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	maxFailureBody           = 64 * 1024
	policyFailureSampleLimit = 5
)

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneAccountModelPolicySummary(policy *AccountModelPolicySummary) *AccountModelPolicySummary {
	if policy == nil {
		return nil
	}
	clone := *policy
	clone.Models = append([]string(nil), policy.Models...)
	return &clone
}

func accountMetadataIdentity(account Account) string {
	provider := deduplicationProviderFamily(firstNonEmpty(account.Provider, account.Type))
	if provider == "" {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(account.AuthID))
	kind := "auth"
	if value == "" {
		value, kind = normalizeDeduplicationEmail(account.Email), "email"
	}
	if value == "" {
		value, kind = strings.TrimSpace(account.ID), "index"
	}
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(provider + "\x00" + kind + ":" + value))
	return hex.EncodeToString(digest[:])
}

func mapKeys[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func boundedCounter(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1_000_000 {
		return 1_000_000
	}
	return value
}

func boundedHTTPStatus(value int) int {
	if value < 100 || value > 599 {
		return 0
	}
	return value
}

func safeModelProbeReason(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "model_response_ok", "credential_response_ok", "authentication_failed", "quota_limited",
		"model_not_found", "unsupported_provider", "model_blocked_by_account_policy",
		"request_timeout", "upstream_unavailable", "invalid_response", "unconfirmed_upstream_response":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "invalid_response"
	}
}

func normalizedFailureText(body string) string {
	if len(body) > maxFailureBody {
		body = body[:maxFailureBody]
	}
	body = strings.ToLower(strings.TrimSpace(body))
	if body == "" {
		return ""
	}
	var document any
	if json.Unmarshal([]byte(body), &document) == nil {
		parts := make([]string, 0, 6)
		collectFailureStrings(document, &parts, 0)
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return body
}

func collectFailureStrings(value any, parts *[]string, depth int) {
	if depth > 4 || len(*parts) >= 16 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"code", "type", "error", "message", "detail", "reason"} {
			if child, exists := typed[key]; exists {
				collectFailureStrings(child, parts, depth+1)
			}
		}
	case []any:
		for _, child := range typed {
			collectFailureStrings(child, parts, depth+1)
		}
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		if text != "" && len(text) <= 2_048 {
			*parts = append(*parts, text)
		}
	}
}
