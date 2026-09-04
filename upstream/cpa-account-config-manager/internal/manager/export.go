package manager

import (
	"encoding/csv"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"cpa-account-config-manager/internal/cpaapi"
)

type exportDownload struct {
	Filename    string
	ContentType string
	Body        []byte
	Credential  bool
	Exported    int
	Skipped     int
}

func writeSafeCSVRow(writer *csv.Writer, values []string) {
	row := make([]string, len(values))
	for index, value := range values {
		row[index] = neutralizeCSVFormula(value)
	}
	_ = writer.Write(row)
}

func neutralizeCSVFormula(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func exportDownloadResponse(download exportDownload) cpaapi.ManagementResponse {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": download.Filename})
	headers := http.Header{
		"Content-Type":                  []string{download.ContentType},
		"Content-Disposition":           []string{disposition},
		"X-Content-Type-Options":        []string{"nosniff"},
		"Access-Control-Expose-Headers": []string{"Content-Disposition, X-Exported-Accounts, X-Skipped-Accounts"},
	}
	if download.Credential {
		headers.Set("Cache-Control", "no-store, private, max-age=0")
		headers.Set("Pragma", "no-cache")
		headers.Set("Expires", "0")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Exported-Accounts", strconv.Itoa(download.Exported))
		headers.Set("X-Skipped-Accounts", strconv.Itoa(download.Skipped))
	}
	return cpaapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       download.Body,
	}
}
