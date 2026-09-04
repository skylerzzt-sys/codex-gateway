package manager

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

func (a *App) handleExportAccounts(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	format, err := credentialExportFormatFromValues(req.Query)
	if err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	query, err := listQueryFromValues(req.Query)
	if err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	collection, err := a.accounts.ExportCredentialSources(ctx, query.Filters)
	if err != nil {
		if errors.Is(err, ErrCredentialExportTooLarge) {
			return jsonResponse(http.StatusRequestEntityTooLarge, map[string]any{"error": err.Error()})
		}
		if errors.Is(err, ErrCredentialExportNoAccounts) {
			return jsonResponse(http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		}
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to export accounts"})
	}
	defer clearCredentialExportCollection(&collection)
	download, err := renderCredentialExport(format, collection, time.Now().UTC())
	if err != nil {
		return jsonResponse(http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}
	return exportDownloadResponse(download)
}
