import {
  Activity,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronLeft,
  ChevronRight,
  CopyCheck,
  Download,
  Eye,
  FileCog,
  Github,
  CircleHelp,
  LoaderCircle,
  LockKeyhole,
  Power,
  PowerOff,
  Pencil,
  RefreshCw,
	RotateCcw,
  ScrollText,
  Settings2,
  ShieldCheck,
  Trash2,
  UserPlus,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "./api/client";
import { AccountDetailsDialog } from "./components/AccountDetailsDialog";
import { AccountActionsMenu } from "./components/AccountActionsMenu";
import { AccountDeduplicationDialog } from "./components/AccountDeduplicationDialog";
import { AccountEditor } from "./components/AccountEditor";
import { AccountUsageCell } from "./components/AccountUsageCell";
import { AgentIdentitySessionLogin } from "./components/AgentIdentitySessionLogin";
import { ExportDialog } from "./components/ExportDialog";
import { IconButton } from "./components/IconButton";
import { ImportDialog } from "./components/ImportDialog";
import { OperationLogWorkspace } from "./components/OperationLogWorkspace";
import { OtherSettingsWorkspace } from "./components/OtherSettingsWorkspace";
import { LoginDialog } from "./components/LoginDialog";
import { ModelTestDialog } from "./components/ModelTestDialog";
import { Modal } from "./components/Modal";
import { DeleteAccountDialog } from "./components/DeleteAccountDialog";
import { operatorMessage } from "./format/operatorMessage";
import { accountState, accountStateLabel, technicalLabel } from "./format/accountDisplay";
import { useI18n, type Locale } from "./i18n";
import { translateUI, type UIMessageKey } from "./i18n/uiText";
import {
  ACCOUNT_PAGE_SIZE_OPTIONS,
  DEFAULT_ACCOUNT_PAGE_SIZE,
  isAccountPageSize,
  readAccountPageSize,
  writeAccountPageSize,
} from "./store/accountPageSize";
import {
  readAccountSort,
  writeAccountSort,
  type AccountSort,
  type AccountSortField,
} from "./store/accountSort";
import { readPanelAuth } from "./store/panelAuth";
import { clearSession, setSession } from "./store/session";
import type {
  Account,
  AccountDeletePreview,
  AccountDeduplicationOptions,
  AccountDeduplicationPreview,
  AccountExportFormat,
  AccountFilters,
  AccountListResponse,
  AccountPatch,
  ExportFormat,
  ExperimentalSettings,
  ImportPreview,
  ImportResult,
  ModelTestResult,
	PersonalGatewayRouting,
	GatewaySelection,
  TargetScope,
} from "./types";
import { accountConcurrencyLimitLabel } from "./accountConcurrency";

const exportFormatLabels: Record<ExportFormat, string> = {
  cpa: "CPA",
  sub2api: "sub2api",
  cockpit: "Cockpit",
  "9router": "9router",
  codex: "Codex",
  axonhub: "AxonHub",
  codexmanager: "Codex-Manager",
  json: "JSON",
  csv: "CSV",
  jsonl: "JSON Lines",
};

const emptyAccountFilters: AccountFilters = {};

const emptyGatewayRouting: PersonalGatewayRouting = {
	configured: false,
	account_a_id: "",
	account_b_id: "",
	mode: "auto",
};

const gatewayModeOptions: Array<{ selection: GatewaySelection; label: UIMessageKey }> = [
	{ selection: "auto", label: "ui.gateway_auto_oauth" },
	{ selection: "force_a", label: "ui.gateway_account_a" },
	{ selection: "force_b", label: "ui.gateway_account_b" },
];

function formatLabel(format: ExportFormat): string {
  return exportFormatLabels[format];
}

const defaultDeduplicationOptions: AccountDeduplicationOptions = {
  ignore_account_id: false,
  exclude_team_accounts: false,
};

interface EditorContext {
  title: UIMessageKey;
  scopeLabel: string;
	scope: TargetScope;
	accountID?: string;
}

const agentIdentityLoginStatePattern = /^[A-Za-z0-9._-]{1,256}$/;

function queryAgentIdentityLoginState(): string | null | undefined {
  const parameters = new URLSearchParams(window.location.search);
  if (!parameters.has("agent_identity_login")) return undefined;
  const state = parameters.get("agent_identity_login")?.trim() ?? "";
  return agentIdentityLoginStatePattern.test(state) ? state : null;
}

export default function App() {
  const agentIdentityLoginState = queryAgentIdentityLoginState();
  if (agentIdentityLoginState !== undefined) return <AgentIdentitySessionLogin loginState={agentIdentityLoginState} />;
  return <AccountManagerApp />;
}

function AccountManagerApp() {
  const { locale, tx, formatDateTime } = useI18n();
  const [authState, setAuthState] = useState<"booting" | "login" | "ready">("booting");
  const [activeView, setActiveView] = useState<"accounts" | "operations" | "settings">("accounts");
  const [authLoading, setAuthLoading] = useState(false);
  const [authError, setAuthError] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(readAccountPageSize);
  const [accountSort, setAccountSort] = useState<AccountSort>(readAccountSort);
  const [data, setData] = useState<AccountListResponse>({ accounts: [], total: 0, page: 1, page_size: DEFAULT_ACCOUNT_PAGE_SIZE, pages: 0, account_concurrency: { supported: false, host_schema_version: 1, required_schema_version: 2, reason: "host_schema_v2_required" } });
	const [gatewayRouting, setGatewayRouting] = useState<PersonalGatewayRouting>(emptyGatewayRouting);
	const [gatewayRoutingBusy, setGatewayRoutingBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const [editorContext, setEditorContext] = useState<EditorContext | null>(null);
  const [detailAccount, setDetailAccount] = useState<Account | null>(null);
	const [quotaMetadataBusy, setQuotaMetadataBusy] = useState<Record<string, "refresh" | "reset">>({});
	const [tokenRefreshBusy, setTokenRefreshBusy] = useState<Record<string, boolean>>({});
	const [quotaResetTarget, setQuotaResetTarget] = useState<Account | null>(null);
  const [modelTestTarget, setModelTestTarget] = useState<Account | null>(null);
  const [modelTestResult, setModelTestResult] = useState<ModelTestResult | null>(null);
  const [modelTesting, setModelTesting] = useState(false);
  const [modelTestError, setModelTestError] = useState("");
  const [modelTestExperimentalAvailable, setModelTestExperimentalAvailable] = useState(false);
  const [weeklyOverdraftEnabled, setWeeklyOverdraftEnabled] = useState(false);
  const [sub2APICreditUsageEnabled, setSub2APICreditUsageEnabled] = useState(false);
  const modelTestExperimentRequest = useRef(0);
  const [deleteTarget, setDeleteTarget] = useState<Account | null>(null);
  const [deletePreview, setDeletePreview] = useState<AccountDeletePreview | null>(null);
  const [deletePreviewing, setDeletePreviewing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [deduplicationPreview, setDeduplicationPreview] = useState<AccountDeduplicationPreview | null>(null);
  const [deduplicationLoading, setDeduplicationLoading] = useState(false);
  const [deduplicationReviewing, setDeduplicationReviewing] = useState(false);
  const [deduplicationError, setDeduplicationError] = useState("");
  const deduplicationRequest = useRef(0);
  const [importOpen, setImportOpen] = useState(false);
  const [importPreview, setImportPreview] = useState<ImportPreview | null>(null);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);
  const [importPreviewing, setImportPreviewing] = useState(false);
  const [importStarting, setImportStarting] = useState(false);
  const [importError, setImportError] = useState("");
  const [exportTarget, setExportTarget] = useState<"accounts" | null>(null);
  const [accountExportScope, setAccountExportScope] = useState<TargetScope | null>(null);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState("");
  const [notice, setNotice] = useState("");
  const accountRequest = useRef(0);
  const skipInitialAccountRefresh = useRef(false);
  const deleteRequest = useRef(0);

  const apiFilters = emptyAccountFilters;

  useEffect(() => {
    writeAccountSort(accountSort);
  }, [accountSort]);

  useEffect(() => {
    let active = true;
    let settingsTimer = 0;
    const bootstrap = async () => {
      const panelAuth = readPanelAuth();
      if (!panelAuth) {
        if (active) setAuthState("login");
        return;
      }
      setSession(panelAuth.apiBase, panelAuth.managementKey);
      try {
        const response = await api.listAccounts(1, pageSize, apiFilters, accountSort);
        if (!active) return;
        setData(response);
		skipInitialAccountRefresh.current = true;
        setAuthState("ready");
		void api.getPersonalGatewayRouting().then((routing) => {
			if (active) setGatewayRouting(routing);
		}).catch(() => undefined);
        settingsTimer = window.setTimeout(() => {
          void api.getExperimentalSettings().then(({ settings }) => {
            if (active) {
              setWeeklyOverdraftEnabled(settings.weekly_overdraft_enabled === true);
              setSub2APICreditUsageEnabled(settings.sub2api_credit_usage_enabled === true);
            }
          }).catch((error) => {
            if (!active) return;
            if (error instanceof api.APIError && error.status === 401) {
              clearSession();
              setAuthState("login");
              setAuthError(operatorMessage(error.message, locale));
              return;
            }
            setNotice(operatorMessage(error instanceof Error ? error.message : "ui.settings_persistence_failed", locale));
          });
        }, 0);
      } catch {
        clearSession();
        if (active) setAuthState("login");
      }
    };
    void bootstrap();
    return () => {
      active = false;
      window.clearTimeout(settingsTimer);
    };
  }, []);

  const handleAPIError = useCallback((error: unknown) => {
    if (error instanceof api.APIError && error.status === 401) {
      clearSession();
      setAuthState("login");
      setAuthError(operatorMessage(error.message, locale));
      return;
    }
    setNotice(errorText(error, locale));
  }, [locale]);

  const handleExperimentalSettingsChange = useCallback((settings: ExperimentalSettings) => {
    setWeeklyOverdraftEnabled(settings.weekly_overdraft_enabled === true);
    setSub2APICreditUsageEnabled(settings.sub2api_credit_usage_enabled === true);
  }, []);

	const gatewaySelection: GatewaySelection = gatewayRouting.mode;
	const selectGatewayMode = async (selection: GatewaySelection) => {
		if (gatewayRoutingBusy || !gatewayRouting.configured) return;
		setGatewayRoutingBusy(true);
		try {
			const routing = await api.savePersonalGatewaySelection(selection);
			setGatewayRouting(routing);
			const labels: Record<GatewaySelection, UIMessageKey> = {
				auto: "ui.gateway_auto_oauth",
				force_a: "ui.gateway_account_a",
				force_b: "ui.gateway_account_b",
			};
			setNotice(tx("ui.gateway_mode_selected", { mode: tx(labels[selection]) }));
		} catch (error) {
			handleAPIError(error);
		} finally {
			setGatewayRoutingBusy(false);
		}
	};

  useEffect(() => {
    if (authState !== "ready") {
      setWeeklyOverdraftEnabled(false);
      setSub2APICreditUsageEnabled(false);
    }
  }, [authState]);

  const refreshAccounts = useCallback(async (silent = false, requestedPage = page, requestedFilters: AccountFilters = apiFilters, requestedSort: AccountSort = accountSort) => {
    if (authState !== "ready") return;
    const requestID = accountRequest.current + 1;
    accountRequest.current = requestID;
    if (!silent) setLoading(true);
    try {
      const response = await api.listAccounts(requestedPage, pageSize, requestedFilters, requestedSort);
      if (requestID !== accountRequest.current) return;
      setData(response);
      if (response.pages > 0 && requestedPage > response.pages) setPage(response.pages);
    } catch (error) {
      if (requestID !== accountRequest.current) return;
      handleAPIError(error);
    } finally {
      if (requestID === accountRequest.current) setLoading(false);
    }
  }, [accountSort, apiFilters, authState, handleAPIError, page, pageSize]);

	const setQuotaBusy = (accountID: string, action?: "refresh" | "reset") => {
		setQuotaMetadataBusy((current) => {
			const next = { ...current };
			if (action) next[accountID] = action;
			else delete next[accountID];
			return next;
		});
	};

	const refreshAccountToken = async (account: Account) => {
		if (!account.editable || tokenRefreshBusy[account.id]) return;
		setTokenRefreshBusy((current) => ({ ...current, [account.id]: true }));
		try {
			const result = await api.refreshAccountToken(account.id);
			setData((current) => ({
				...current,
				accounts: current.accounts.map((entry) => entry.id === account.id
					? { ...entry, last_refresh: result.refreshed_at, updated_at: result.refreshed_at }
					: entry),
			}));
			setNotice(tx("ui.token_refreshed_for_account", { account: account.label || account.email || account.name || account.id }));
		} catch (error) {
			handleAPIError(error);
		} finally {
			setTokenRefreshBusy((current) => {
				const next = { ...current };
				delete next[account.id];
				return next;
			});
		}
	};

	const refreshQuotaMetadata = async (account: Account) => {
		setQuotaBusy(account.id, "refresh");
		try {
			const result = await api.refreshAccountQuotaMetadata(account.id);
			await refreshAccounts(true);
			setNotice(tx(result.warning ? "ui.quota_metadata_refreshed_with_warning" : "ui.quota_metadata_refreshed", { account: account.label || account.email || account.name || account.id }));
		} catch (error) {
			handleAPIError(error);
		} finally {
			setQuotaBusy(account.id);
		}
	};

	const confirmQuotaReset = async () => {
		if (!quotaResetTarget) return;
		const account = quotaResetTarget;
		setQuotaBusy(account.id, "reset");
		try {
			const result = await api.useAccountActiveReset(account.id);
			await refreshAccounts(true);
			setQuotaResetTarget(null);
			setNotice(tx(result.warning ? "ui.active_reset_succeeded_with_warning" : "ui.active_reset_succeeded", { account: account.label || account.email || account.name || account.id }));
		} catch (error) {
			handleAPIError(error);
		} finally {
			setQuotaBusy(account.id);
		}
	};

  useEffect(() => {
    if (activeView !== "accounts") return;
    if (skipInitialAccountRefresh.current) {
      skipInitialAccountRefresh.current = false;
      return;
    }
    void refreshAccounts();
  }, [activeView, refreshAccounts]);

  useEffect(() => {
    if (authState !== "ready" || importResult?.state !== "running") return;
    let cancelled = false;
    let timer = 0;
    const poll = async () => {
      try {
        const result = await api.getImportStatus();
        if (cancelled) return;
        setImportResult(result);
        if (result.state === "running") {
          timer = window.setTimeout(poll, 1000);
          return;
        }
        setNotice(result.failed || result.skipped
          ? tx("ui.added_count_accounts_failed_not_written", { count: result.imported, failed: result.failed + result.skipped })
          : tx("ui.added_count_accounts", { count: result.imported }));
        void refreshAccounts();
      } catch (error) {
        if (!cancelled) {
          setImportError(errorText(error, locale));
          if (error instanceof api.APIError && error.status === 401) handleAPIError(error);
        }
      }
    };
    timer = window.setTimeout(poll, 500);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [authState, handleAPIError, importResult?.state, locale, refreshAccounts, tx]);

  const login = async (baseURL: string, managementKey: string) => {
    setAuthLoading(true);
    setAuthError("");
    setSession(baseURL, managementKey);
    try {
      const response = await api.listAccounts(1, pageSize, apiFilters, accountSort);
      setData(response);
      skipInitialAccountRefresh.current = true;
      setAuthState("ready");
		void api.getPersonalGatewayRouting().then(setGatewayRouting).catch(handleAPIError);
      window.setTimeout(() => {
        void api.getExperimentalSettings().then(({ settings }) => handleExperimentalSettingsChange(settings)).catch(handleAPIError);
      }, 0);
    } catch (error) {
      clearSession();
      setAuthError(error instanceof Error ? operatorMessage(error.message, locale) : tx("ui.authentication_failed"));
    } finally {
      setAuthLoading(false);
    }
  };

  const updateAccountSort = (field: AccountSortField) => {
    setAccountSort((current) => ({
      field,
      order: current.field === field && current.order === "asc" ? "desc" : "asc",
    }));
    setPage(1);
  };

  const updatePageSize = (value: string) => {
    const nextPageSize = Number(value);
    if (!isAccountPageSize(nextPageSize) || nextPageSize === pageSize) return;
    writeAccountPageSize(nextPageSize);
    setPageSize(nextPageSize);
    setPage(1);
  };

  const loadAccountDeduplication = async (options: AccountDeduplicationOptions, keepDialogOpen: boolean) => {
    const requestID = ++deduplicationRequest.current;
    setDeduplicationLoading(true);
    setDeduplicationError("");
    try {
      const nextPreview = await api.scanAccountDuplicates(options);
      if (deduplicationRequest.current === requestID) setDeduplicationPreview(nextPreview);
    } catch (error) {
      if (deduplicationRequest.current !== requestID) return;
      if (keepDialogOpen && !(error instanceof api.APIError && error.status === 401)) {
        setDeduplicationError(errorText(error, locale));
      } else {
        handleAPIError(error);
      }
    } finally {
      if (deduplicationRequest.current === requestID) setDeduplicationLoading(false);
    }
  };

  const openAccountDeduplication = async () => {
    await loadAccountDeduplication(defaultDeduplicationOptions, false);
  };

  const reviewDuplicateDeletions = async (accountIDs: string[]) => {
    if (accountIDs.length !== 1) return;
    const account = data.accounts.find((candidate) => candidate.id === accountIDs[0]);
    if (account) await openDelete(account);
  };

  const updateAccountState = async (account: Account, disabled: boolean) => {
    try {
      await api.updateAccount(account.id, { disabled });
      setData((current) => ({ ...current, accounts: current.accounts.map((candidate) => candidate.id === account.id ? { ...candidate, disabled } : candidate) }));
    } catch (error) {
      handleAPIError(error);
    }
  };

  const openAccountEditor = (account: Account) => {
    if (!account.editable) return;
    setDetailAccount(null);
    setEditorContext({
      title: "ui.edit_account",
      scopeLabel: account.label || account.email || account.name || account.id,
      scope: { mode: "selected", ids: [account.id] },
		accountID: account.id,
    });
  };

  const openModelTest = (account: Account) => {
    const requestID = modelTestExperimentRequest.current + 1;
    modelTestExperimentRequest.current = requestID;
    setModelTestTarget(account);
    setModelTestResult(null);
    setModelTestError("");
    setModelTestExperimentalAvailable(weeklyOverdraftEnabled);
    void api.getExperimentalSettings().then((snapshot) => {
      if (modelTestExperimentRequest.current !== requestID) return;
      setModelTestExperimentalAvailable(snapshot.settings.weekly_overdraft_enabled === true);
    }).catch((error) => {
      if (modelTestExperimentRequest.current !== requestID) return;
      if (error instanceof api.APIError && error.status === 401) {
        setModelTestTarget(null);
        handleAPIError(error);
      } else {
        setModelTestError(errorText(error, locale));
      }
    });
  };

  const closeModelTest = () => {
    if (modelTesting) return;
    modelTestExperimentRequest.current += 1;
    setModelTestTarget(null);
    setModelTestResult(null);
    setModelTestError("");
    setModelTestExperimentalAvailable(false);
  };

  const runModelTest = async (model: string, experimentalWeeklyOverdraft = false) => {
    if (!modelTestTarget) return;
    setModelTesting(true);
    setModelTestError("");
    setModelTestResult(null);
    try {
      setModelTestResult(await api.testAccountModel(modelTestTarget.id, model, experimentalWeeklyOverdraft));
    } catch (error) {
      if (error instanceof api.APIError && error.status === 401) {
        setModelTestTarget(null);
        handleAPIError(error);
      } else {
        setModelTestError(errorText(error, locale));
      }
    } finally {
      setModelTesting(false);
    }
  };

  const closeDelete = () => {
    deleteRequest.current += 1;
    setDeleteTarget(null);
    setDeletePreview(null);
    setDeletePreviewing(false);
    setDeleting(false);
    setDeleteError("");
  };

  const openDelete = async (account: Account) => {
    if (!account.editable) return;
    const requestID = deleteRequest.current + 1;
    deleteRequest.current = requestID;
    setDeleteTarget(account);
    setDeletePreview(null);
    setDeleteError("");
    setDeletePreviewing(true);
    try {
      const response = await api.createAccountDeletePreview(account.id);
      if (requestID === deleteRequest.current) setDeletePreview(response);
    } catch (error) {
      if (requestID !== deleteRequest.current) return;
      if (error instanceof api.APIError && error.status === 401) {
        closeDelete();
        handleAPIError(error);
      } else {
        setDeleteError(errorText(error, locale));
      }
    } finally {
      if (requestID === deleteRequest.current) setDeletePreviewing(false);
    }
  };

  const confirmDelete = async () => {
    if (!deletePreview) return;
    const deletedID = deletePreview.account.id;
    setDeleting(true);
    setDeleteError("");
    try {
      const result = await api.deleteAccount(deletePreview.id);
      closeDelete();
      setNotice(tx("ui.deleted_account_account", { account: result.account.label || result.account.email || result.account.name }));
      await refreshAccounts();
    } catch (error) {
      if (error instanceof api.APIError && error.status === 401) {
        closeDelete();
        handleAPIError(error);
      } else {
        setDeleteError(errorText(error, locale));
      }
    } finally {
      setDeleting(false);
    }
  };

  const openImport = () => {
    setImportPreview(null);
    setImportResult(null);
    setImportError("");
    setImportOpen(true);
    void api.getImportStatus().then((result) => {
      if (result.state === "running") setImportResult(result);
    }).catch((error) => {
      if (error instanceof api.APIError && error.status === 401) handleAPIError(error);
    });
  };

  const closeImport = () => {
    setImportOpen(false);
    setImportPreview(null);
    setImportResult(null);
    setImportError("");
  };

  const resetImport = () => {
    setImportPreview(null);
    setImportResult(null);
    setImportError("");
  };

  const previewImport = async (files: File[]) => {
    setImportPreviewing(true);
    setImportError("");
    setImportResult(null);
    try {
      setImportPreview(await api.createImportPreview(files));
    } catch (error) {
      if (error instanceof api.APIError && error.status === 401) {
        closeImport();
        handleAPIError(error);
      } else {
        setImportError(errorText(error, locale));
      }
    } finally {
      setImportPreviewing(false);
    }
  };

  const confirmImport = async () => {
    if (!importPreview) return;
    setImportStarting(true);
    setImportError("");
    try {
      const result = await api.startImport(importPreview.id);
      setImportPreview(null);
      setImportResult(result);
      setNotice(tx("ui.import_started_in_background"));
    } catch (error) {
      if (error instanceof api.APIError && error.status === 401) {
        closeImport();
        handleAPIError(error);
      } else {
        setImportError(errorText(error, locale));
      }
    } finally {
      setImportStarting(false);
    }
  };

  const openExport = () => {
    setExportTarget("accounts");
    setAccountExportScope({ mode: "filtered", filters: apiFilters });
    setExportError("");
  };

  const closeExport = () => {
    setExportTarget(null);
    setAccountExportScope(null);
    setExportError("");
  };

  const confirmExport = async (format: ExportFormat) => {
    if (!exportTarget) return;
    setExporting(true);
    setExportError("");
    try {
      const result = await api.downloadExport("accounts", format as AccountExportFormat, accountExportScope ?? { mode: "filtered", filters: apiFilters });
      setNotice(tx("ui.downloaded_format_credentials_for_count_accounts_skipped_skipped", {
        format: formatLabel(format),
        count: result.exported ?? 0,
        skipped: result.skipped ?? 0,
      }));
      closeExport();
    } catch (error) {
      if (error instanceof api.APIError && error.status === 401) {
        closeExport();
        handleAPIError(error);
      } else {
        setExportError(errorText(error, locale));
      }
    } finally {
      setExporting(false);
    }
  };

  const exportCount = data.total;

  return (
    <div className="app-shell">
      <div className="page-frame">
        <header className="app-header">
          <div className="brand-block">
            <span className="brand-icon"><FileCog size={21} /></span>
            <div><h1>{tx("ui.account_management")}</h1><span>CPA Account Config Manager</span></div>
          </div>
        </header>

        <div className="workspace-bar">
          <nav className="workspace-tabs" aria-label={tx("ui.account_management_views")}>
            <button type="button" className={activeView === "accounts" ? "active" : ""} aria-current={activeView === "accounts" ? "page" : undefined} onClick={() => setActiveView("accounts")}><FileCog size={16} />{tx("ui.accounts")}</button>
            <button type="button" className={activeView === "operations" ? "active" : ""} aria-current={activeView === "operations" ? "page" : undefined} onClick={() => setActiveView("operations")}><ScrollText size={16} />{tx("ui.operation_log")}</button>
            <button type="button" className={activeView === "settings" ? "active" : ""} aria-current={activeView === "settings" ? "page" : undefined} onClick={() => setActiveView("settings")}><Settings2 size={16} />{tx("ui.other_settings")}</button>
          </nav>
          <div className="workspace-controls">
            <div className="header-status">
              <span><ShieldCheck size={15} />{tx("ui.count_accounts", { count: data.total })}</span>
            </div>
            <div className="header-actions">
              {activeView === "accounts" ? <>
                <button className="button button-primary header-add-account" type="button" title={tx("ui.add_accounts")} aria-label={tx("ui.add_accounts")} onClick={openImport}><UserPlus size={16} /><span>{tx("ui.add_accounts")}</span></button>
                <button className="button header-deduplicate-account" type="button" title={tx("ui.deduplicate_accounts")} aria-label={tx("ui.deduplicate_accounts")} disabled={deduplicationLoading} onClick={() => void openAccountDeduplication()}>{deduplicationLoading ? <LoaderCircle className="spin" size={16} /> : <CopyCheck size={16} />}<span>{tx("ui.deduplicate_accounts")}</span></button>
                <IconButton className="export-action" label={tx("ui.download_filtered_credentials")} onClick={openExport}><Download size={17} /></IconButton>
                <IconButton label={tx("ui.refresh_accounts")} onClick={() => void refreshAccounts()} disabled={loading}><RefreshCw className={loading ? "spin" : ""} size={17} /></IconButton>
              </> : null}
              <a className="icon-button" href="https://github.com/Mxucc/cpa-account-config-manager/" target="_blank" rel="noopener noreferrer" aria-label={tx("ui.open_project_on_github")} title={tx("ui.open_project_on_github")}><Github size={17} /></a>
            </div>
          </div>
        </div>

        {notice ? <div className="notice-bar global-notice" role="alert"><span>{notice}</span><IconButton label={tx("ui.dismiss_notification")} onClick={() => setNotice("")}><X size={15} /></IconButton></div> : null}

        {activeView === "accounts" ? (
        <section className="account-panel">
          <main className="account-workspace">
        {gatewayRouting.configured ? (
          <section className="gateway-mode-panel" aria-label={tx("ui.gateway_execution_channel")}>
            <strong>{tx("ui.gateway_execution_channel")}</strong>
            <div className="gateway-mode-options">
              {gatewayModeOptions.map((option) => {
                const label = tx(option.label);
                return (
                  <label key={option.selection} className={gatewaySelection === option.selection ? "is-selected" : ""}>
                    <input
                      type="radio"
                      name="personal-gateway-mode"
                      checked={gatewaySelection === option.selection}
                      disabled={gatewayRoutingBusy}
                      onChange={() => void selectGatewayMode(option.selection)}
                      aria-label={tx("ui.select_gateway_mode", { mode: label })}
                    />
                    <span>{label}</span>
                  </label>
                );
              })}
            </div>
          </section>
        ) : null}
        <div className="table-meta">
          <div className="table-title"><span>{tx("ui.account_list")}</span><strong>{data.total}</strong></div>
          <span>{tx("ui.count_records_page_page_slash_pages", { count: data.total, page: data.page || 1, pages: data.pages || 1 })}</span>
        </div>
        <div className="table-scroll">
          <table className="account-table">
            <colgroup>
              <col className="col-identity" /><col className="col-provider" />
								<col className="col-type" /><col className="col-activity" /><col className="col-active-reset" /><col className="col-concurrency" />
								<col className="col-state" /><col className="col-routing" /><col className="col-actions" />
            </colgroup>
            <thead>
              <tr>
								<SortableAccountHeader className="identity-header" field="account" label={tx("ui.accounts")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="provider" label={tx("ui.provider")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="type" label={tx("ui.type")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="usage" label={tx("ui.usage")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="active_reset_count" label={tx("ui.active_reset_count")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="concurrency" label={tx("ui.account_concurrency")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="status" label={tx("ui.status")} sort={accountSort} onSort={updateAccountSort} />
								<SortableAccountHeader field="routing" label={tx("ui.routing")} sort={accountSort} onSort={updateAccountSort} />
								<th className="actions-header">{tx("ui.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {loading ? <LoadingRows /> : <>
				{data.accounts.map((account) => {
                const identity = account.label || account.email || account.name || account.id;
                const gatewaySlot = [account.id, account.name].includes(gatewayRouting.account_a_id) ? "a"
                  : [account.id, account.name].includes(gatewayRouting.account_b_id) ? "b"
                  : null;
                const readOnlyReason = operatorMessage(account.read_only_reason, locale) || tx("ui.this_account_is_read_only");
                return (
                <tr key={account.id} className={!account.editable ? "is-readonly" : ""}>
                  <td className="identity-column-cell">
                    <div className="identity-cell">
                      <div className="identity-title">
                        <strong>{account.label || account.email || account.name || account.id}</strong>
                        {gatewaySlot === "a" ? <span className="gateway-account-badge gateway-a" title={tx("ui.gateway_account_a")}>A</span> : null}
                        {gatewaySlot === "b" ? <span className="gateway-account-badge gateway-b" title={tx("ui.gateway_account_b")}>B</span> : null}
                      </div>
                      <span>{account.email && account.label !== account.email ? account.email : account.name}</span>
                      {account.note ? <small>{account.note}</small> : null}
                    </div>
                  </td>
                  <td><span className="provider-tag">{technicalLabel(account.provider || account.type)}</span></td>
                  <td><AccountTypeCell account={account} /></td>
                  <td><AccountUsageCell account={account} weeklyOverdraftEnabled={weeklyOverdraftEnabled} creditUsageEnabled={sub2APICreditUsageEnabled} /></td>
									<td><AccountQuotaMetadataCell account={account} busy={quotaMetadataBusy[account.id]} onRefresh={() => void refreshQuotaMetadata(account)} onReset={() => setQuotaResetTarget(account)} /></td>
									<td><AccountConcurrencyCell account={account} /></td>
                  <td><StateCell account={account} /></td>
                  <td><RoutingCell account={account} /></td>
                  <td className="actions-cell">
                    <div className="row-actions">
                      <IconButton label={tx("ui.view_account", { account: identity })} onClick={() => setDetailAccount(account)}><Eye size={15} /></IconButton>
                      <IconButton label={tx("ui.test_model_for_account", { account: identity })} onClick={() => openModelTest(account)}><Activity size={15} /></IconButton>
                      <IconButton label={account.editable ? tx("ui.edit_account_2", { account: identity }) : readOnlyReason} disabled={!account.editable} onClick={() => openAccountEditor(account)}><Pencil size={15} /></IconButton>
                      <IconButton
                        className="row-enable-action"
                        label={tx("ui.enable_account_2", { account: identity })}
                        title={!account.editable ? readOnlyReason : undefined}
                        disabled={!account.editable || !account.disabled}
                        onClick={() => void updateAccountState(account, false)}
                      ><Power size={15} /></IconButton>
                      <IconButton
                        className="row-disable-action"
                        label={tx("ui.disable_account_2", { account: identity })}
                        title={!account.editable ? readOnlyReason : undefined}
                        disabled={!account.editable || account.disabled}
                        onClick={() => void updateAccountState(account, true)}
                      ><PowerOff size={15} /></IconButton>
											<AccountActionsMenu
												label={tx("ui.more_actions_for_account", { account: identity })}
												menuLabel={tx("ui.account_more_actions")}
												refreshLabel={tx("ui.refresh_token")}
												deleteLabel={tx("ui.delete_account")}
												disabled={!account.editable}
												disabledReason={readOnlyReason}
												refreshing={Boolean(tokenRefreshBusy[account.id])}
												onRefresh={() => void refreshAccountToken(account)}
												onDelete={() => void openDelete(account)}
											/>
                    </div>
                  </td>
                </tr>
                );
				})}
			  </>}
            </tbody>
          </table>
          {!loading && data.accounts.length === 0 ? <div className="empty-state" role="status">{tx("ui.no_matching_accounts")}</div> : null}
        </div>
        <div className="pagination">
          <label className="page-size-control">
            <span>{tx("ui.per_page")}</span>
            <select aria-label={tx("ui.accounts_per_page")} value={pageSize} onChange={(event) => updatePageSize(event.target.value)}>
              {ACCOUNT_PAGE_SIZE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
            </select>
          </label>
          <IconButton label={tx("ui.previous_page")} disabled={page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}><ChevronLeft size={17} /></IconButton>
          <strong>{page}</strong>
          <IconButton label={tx("ui.next_page")} disabled={data.pages === 0 || page >= data.pages} onClick={() => setPage((value) => value + 1)}><ChevronRight size={17} /></IconButton>
        </div>
          </main>
        </section>
        ) : activeView === "operations" ? (
          <OperationLogWorkspace
            onAPIError={handleAPIError}
            onNotice={setNotice}
            activeJobIDs={[]}
            onOpenRelatedJob={() => undefined}
          />
        ) : (
          <OtherSettingsWorkspace onAPIError={handleAPIError} onNotice={setNotice} onExperimentalSettingsChange={handleExperimentalSettingsChange} />
        )}
      </div>

      {authState === "booting" ? <div className="auth-loading"><LoaderCircle className="spin" size={24} /></div> : null}
      {authState === "login" ? <LoginDialog loading={authLoading} error={authError} onSubmit={login} /> : null}
      {editorContext ? <AccountEditor title={editorContext.title} scopeLabel={editorContext.scopeLabel} accountConcurrency={data.account_concurrency} loadModels={() => api.loadAccountModels(editorContext.scope)} loadCurrentConfig={() => api.loadAccountConfig(editorContext.accountID || "")} onLoadError={(error) => { if (error instanceof api.APIError && error.status === 401) { setEditorContext(null); handleAPIError(error); } }} onClose={() => setEditorContext(null)} onSubmit={(patch) => { const accountID = editorContext.accountID || ""; void api.updateAccount(accountID, patch).then(() => refreshAccounts()).then(() => setEditorContext(null)).catch(handleAPIError); }} /> : null}
      {detailAccount ? <AccountDetailsDialog account={detailAccount} creditUsageEnabled={sub2APICreditUsageEnabled} weeklyOverdraftEnabled={weeklyOverdraftEnabled} onClose={() => setDetailAccount(null)} onEdit={() => openAccountEditor(detailAccount)} /> : null}
			{quotaResetTarget ? (
				<Modal
					title={tx("ui.confirm_active_reset")}
					onClose={() => { if (!quotaMetadataBusy[quotaResetTarget.id]) setQuotaResetTarget(null); }}
					footer={(
						<>
							<button className="button" type="button" disabled={Boolean(quotaMetadataBusy[quotaResetTarget.id])} onClick={() => setQuotaResetTarget(null)}>{tx("ui.cancel")}</button>
							<button className="button button-primary" type="button" disabled={Boolean(quotaMetadataBusy[quotaResetTarget.id])} onClick={() => void confirmQuotaReset()}>
								{quotaMetadataBusy[quotaResetTarget.id] ? <LoaderCircle className="spin" size={15} /> : <RotateCcw size={15} />}{tx("ui.use_active_reset")}
							</button>
						</>
					)}
				>
					<p className="confirmation-copy">{tx("ui.confirm_active_reset_description", { account: quotaResetTarget.label || quotaResetTarget.email || quotaResetTarget.name || quotaResetTarget.id })}</p>
				</Modal>
			) : null}
      {modelTestTarget ? <ModelTestDialog key={modelTestTarget.id} account={modelTestTarget} result={modelTestResult} error={modelTestError} testing={modelTesting} experimentalAvailable={modelTestExperimentalAvailable} onClose={closeModelTest} onTest={(model, experimental) => void runModelTest(model, experimental)} /> : null}
      {deleteTarget ? <DeleteAccountDialog key={deleteTarget.id} account={deleteTarget} preview={deletePreview} previewing={deletePreviewing} deleting={deleting} error={deleteError} onClose={closeDelete} onConfirm={() => void confirmDelete()} /> : null}
      {deduplicationPreview ? <AccountDeduplicationDialog preview={deduplicationPreview} loading={deduplicationLoading} reviewing={deduplicationReviewing} error={deduplicationError} onClose={() => { deduplicationRequest.current++; setDeduplicationPreview(null); setDeduplicationError(""); setDeduplicationLoading(false); }} onOptionsChange={(options) => void loadAccountDeduplication(options, true)} onReview={(ids) => void reviewDuplicateDeletions(ids)} /> : null}
                  {importOpen ? <ImportDialog preview={importPreview} result={importResult} previewing={importPreviewing} importing={importStarting} error={importError} onClose={closeImport} onPreview={(files) => void previewImport(files)} onImport={() => void confirmImport()} onReset={resetImport} /> : null}
      {exportTarget ? <ExportDialog kind={exportTarget} count={exportCount} exporting={exporting} error={exportError} onClose={closeExport} onExport={(format) => void confirmExport(format)} /> : null}
    </div>
  );
}

function AccountTypeCell({ account }: { account: Account }) {
	const { tx } = useI18n();
  const primary = account.plan_type || account.account_type || account.type || "-";
  const secondary = account.plan_type ? account.account_type || account.type : "";
	const primaryLabel = primary === "agent_identity" ? tx("ui.agent_identity") : primary === "personal_access_token" ? tx("ui.codex_personal_access_token") : primary;
	const secondaryLabel = secondary === "agent_identity" ? tx("ui.agent_identity") : secondary === "personal_access_token" ? tx("ui.codex_personal_access_token") : secondary;
  return (
    <div className="type-cell" title={secondaryLabel ? `${primaryLabel} / ${secondaryLabel}` : primaryLabel}>
      <strong className="account-plan-type">{primaryLabel}</strong>
      {secondaryLabel && secondaryLabel !== primaryLabel ? <span>{secondaryLabel}</span> : null}
    </div>
  );
}

function SortableAccountHeader({ className, field, label, sort, onSort }: {
	className?: string;
	field: AccountSortField;
	label: string;
	sort: AccountSort;
	onSort: (field: AccountSortField) => void;
}) {
	const { tx } = useI18n();
	const active = sort.field === field;
	const nextOrder = active && sort.order === "asc" ? "desc" : "asc";
	const nextOrderLabel = tx(nextOrder === "asc" ? "ui.ascending" : "ui.descending");
	const currentOrderLabel = tx(sort.order === "asc" ? "ui.ascending" : "ui.descending");
	const buttonLabel = active
		? tx("ui.sort_column_current_next", { column: label, current: currentOrderLabel, next: nextOrderLabel })
		: tx("ui.sort_column_next", { column: label, next: nextOrderLabel });
	return (
		<th className={className} aria-sort={active ? sort.order === "asc" ? "ascending" : "descending" : "none"}>
			<button className={`table-sort-button ${active ? "is-active" : ""}`} type="button" title={buttonLabel} aria-label={buttonLabel} onClick={() => onSort(field)}>
				<span>{label}</span>
				{active ? sort.order === "asc" ? <ArrowUp size={13} aria-hidden="true" /> : <ArrowDown size={13} aria-hidden="true" /> : <ArrowUpDown size={13} aria-hidden="true" />}
			</button>
		</th>
	);
}

function AccountLifecycleTime({ value }: { value?: string }) {
	const { formatDateTime } = useI18n();
	if (!value) return <span className="account-time-empty">-</span>;
	const formatted = formatDateTime(value);
	return <time className="account-lifecycle-time" dateTime={value} title={formatted}>{formatted}</time>;
}

function AccountQuotaMetadataCell({ account, busy, onRefresh, onReset }: { account: Account; busy?: "refresh" | "reset"; onRefresh: () => void; onReset: () => void }) {
	const { tx, formatNumber, formatDateTime } = useI18n();
	const provider = String(account.provider || account.type).trim().toLowerCase();
	const supported = provider === "codex" || provider === "codex-agent-identity";
	const count = account.usage?.codex?.active_reset_count;
	const known = typeof count === "number" && Number.isFinite(count) && count >= 0;
	const observedAt = account.usage?.codex?.metadata_observed_at;
	if (!supported) return <span className="quota-metadata-unsupported">-</span>;
	return (
		<div className="quota-metadata-cell">
			<div className="quota-metadata-value"><strong>{known ? formatNumber(count) : "-"}</strong><IconButton label={tx("ui.refresh_plan_and_active_reset", { account: account.label || account.email || account.name || account.id })} disabled={Boolean(busy)} onClick={onRefresh}>{busy === "refresh" ? <LoaderCircle className="spin" size={14} /> : <RefreshCw size={14} />}</IconButton></div>
			{known && count > 0 ? <button className="quota-reset-button" type="button" disabled={Boolean(busy)} onClick={onReset}>{busy === "reset" ? <LoaderCircle className="spin" size={12} /> : <RotateCcw size={12} />}{tx("ui.use_active_reset")}</button> : null}
			<small>{observedAt ? tx("ui.quota_metadata_collected_at", { time: formatDateTime(observedAt) }) : tx("ui.not_collected")}</small>
		</div>
	);
}

function AccountConcurrencyCell({ account }: { account: Account }) {
	const { tx } = useI18n();
	if (!account.concurrency?.supported) {
		const explanation = tx("ui.account_concurrency_unavailable_old_cpa");
		return <span className="concurrency-unavailable" title={explanation} aria-label={`${tx("ui.unavailable")}: ${explanation}`} tabIndex={0}>{tx("ui.unavailable")}<CircleHelp size={12} aria-hidden="true" /></span>;
	}
	const limit = accountConcurrencyLimitLabel(account.concurrency);
	const saturated = account.concurrency.limit > 0 && account.concurrency.active >= account.concurrency.limit;
	return (
		<div className={`concurrency-cell ${saturated ? "is-saturated" : ""}`} title={tx("ui.account_concurrency_active_limit", { active: account.concurrency.active, limit })}>
			<strong>{account.concurrency.active}</strong><span>/</span><strong>{limit}</strong>
		</div>
	);
}

function StateCell({ account }: { account: Account }) {
  const { locale, tx } = useI18n();
  const status = accountState(account);
  return (
    <div className="state-cell" title={operatorMessage(account.status_message, locale)}>
      <span className={`state-dot state-${status}`} />
      <strong>{accountStateLabel(account, locale)}</strong>
      <span className="state-message">{account.disabled ? tx("ui.account_disabled") : operatorMessage(account.status_message, locale)}</span>
    </div>
  );
}

function RoutingCell({ account }: { account: Account }) {
  const { locale, tx } = useI18n();
  return (
    <div className="routing-cell">
      <code>{account.prefix || tx("ui.default")}</code>
      <span title={account.proxy || (account.proxy_configured ? tx("ui.proxy_configured") : tx("ui.no_proxy"))}>{account.proxy_configured ? <Wifi size={14} /> : <WifiOff size={14} />}</span>
      <span className={account.websockets ? "ws-on" : ""}>WS {account.websockets ? tx("ui.on_2") : tx("ui.off_2")}</span>
      {account.header_count > 0 ? <span>H {account.header_count}</span> : null}
    </div>
  );
}

function LoadingRows() {
	return <>{Array.from({ length: 8 }, (_, index) => <tr className="loading-row" key={index}><td colSpan={15}><span /></td></tr>)}</>;
}

function errorText(error: unknown, locale: Locale = "zh-CN"): string {
  return error instanceof Error ? operatorMessage(error.message, locale) : translateUI(locale, "ui.request_failed");
}
