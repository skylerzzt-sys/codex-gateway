export interface Account {
  id: string;
  auth_id?: string;
  name: string;
  provider?: string;
  type?: string;
  label?: string;
  email?: string;
  project_id?: string;
  account_type?: string;
  plan_type?: string;
  status?: string;
  status_message?: string;
  disabled: boolean;
  unavailable: boolean;
  runtime_only: boolean;
  source?: string;
  note?: string;
  prefix?: string;
  proxy?: string;
  proxy_configured: boolean;
  websockets?: boolean;
  header_names?: string[];
  header_count: number;
  editable: boolean;
  read_only_reason?: string;
  success: number;
  failed: number;
  recent_requests?: RecentRequestEntry[];
  next_retry_after?: string;
  usage?: AccountUsageSnapshot;
  updated_at?: string;
  last_refresh?: string;
	model_policy?: AccountModelPolicySummary;
	concurrency?: AccountConcurrencySummary;
}

export type GatewayMode = "auto" | "force_a" | "force_b";
export type GatewaySelection = GatewayMode;

export interface PersonalGatewayRouting {
	configured: boolean;
	account_a_id: string;
	account_b_id: string;
	mode: GatewayMode;
}

export interface AccountConcurrencyAvailability {
	supported: boolean;
	host_schema_version: number;
	required_schema_version: number;
	reason?: "host_schema_v2_required";
}

export interface AccountConcurrencySummary {
	supported: boolean;
	limit: number;
	active: number;
}

export type ModelPolicyMode = "all" | "allow_only" | "deny_only";

export interface AccountModelPolicySummary {
	mode: ModelPolicyMode;
	models?: string[];
	excluded_count: number;
}

export interface AccountEditableConfig {
	account_id: string;
	disabled: boolean;
	priority: number | null;
	note: string;
	prefix: string;
	proxy: string;
	proxy_configured: boolean;
	websockets: boolean | null;
	header_names: string[];
	model_policy: AccountModelPolicySummary | null;
	concurrency?: AccountConcurrencySummary;
	account_concurrency?: AccountConcurrencyAvailability;
}

export interface ModelPolicyPatch {
	mode: ModelPolicyMode;
	models?: string[];
}

export interface AccountModelOption {
	id: string;
	display_name?: string;
	type?: string;
	owned_by?: string;
}

export interface AccountModelCatalogResponse {
	models: AccountModelOption[];
	current_policy?: AccountModelPolicySummary;
	total: number;
	eligible: number;
	loaded: number;
	failed: number;
	read_only: number;
	missing: number;
	warnings?: string[];
}

export interface RecentRequestEntry {
  time: string;
  success: number;
  failed: number;
}

export interface UsageWindowSnapshot {
  used_percent: number;
  reset_at?: string;
  window_minutes?: number;
	overdraft_active?: boolean;
	overdraft_tokens?: number;
	overdraft_requests?: number;
	overdraft_amount_usd?: number;
	overdraft_rated_requests?: number;
	overdraft_unrated_requests?: number;
	overdraft_started_at?: string;
	overdraft_recover_at?: string;
}

export interface CodexUsageSnapshot {
  five_hour?: UsageWindowSnapshot;
  seven_day?: UsageWindowSnapshot;
	plan_type?: string;
	active_reset_count?: number;
	metadata_observed_at?: string;
  observed_at: string;
}

export interface QuotaMetadataResponse {
	account_id: string;
	plan_type?: string;
	active_reset_count?: number;
	observed_at: string;
	warning?: "active_reset_count_unavailable" | "quota_metadata_refresh_after_reset_unavailable";
	reset_credit_used?: boolean;
}

export interface AccountTokenRefreshResult {
	account_id: string;
	provider?: string;
	refresh_source: "cpa_native" | "plugin_codex";
	refreshed_at: string;
	expires_at?: string;
	refresh_token_rotated: boolean;
}

export interface AccountUsageSnapshot {
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cached_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  total_tokens: number;
  last_request_at?: string;
  updated_at?: string;
  codex?: CodexUsageSnapshot;
  credit?: CreditUsageSnapshot;
}

export interface CreditUsageSnapshot {
  amount_usd: number;
  rated_requests: number;
  unrated_requests: number;
  started_at?: string;
  pricing_updated_at?: string;
  pricing_source?: string;
}

export interface AccountFilters {
  provider?: string;
  type?: string;
  status?: string;
  disabled?: boolean;
  editability?: string;
  source?: string;
  search?: string;
}

export type AccountSortField = "account" | "provider" | "type" | "usage" | "active_reset_count" | "concurrency" | "status" | "routing";
export type AccountSortOrder = "asc" | "desc";

export interface AccountSort {
  field: AccountSortField;
  order: AccountSortOrder;
}

export interface AccountListResponse {
  accounts: Account[];
  total: number;
  page: number;
  page_size: number;
  pages: number;
	account_concurrency?: AccountConcurrencyAvailability;
}

export type AccountDeduplicationMatch = "account_id" | "email" | "multiple";
export type AccountDeduplicationAction = "keep" | "delete" | "skip";

export interface AccountDeduplicationOptions {
  ignore_account_id: boolean;
  exclude_team_accounts: boolean;
}

export interface AccountDeduplicationMember {
  id: string;
  name?: string;
  email?: string;
  provider?: string;
  type?: string;
  plan_type?: string;
  status?: string;
  disabled: boolean;
  unavailable: boolean;
  editable: boolean;
  read_only_reason?: string;
  updated_at?: string;
  last_refresh?: string;
  recommended_action: AccountDeduplicationAction;
}

export interface AccountDeduplicationGroup {
  id: string;
  provider: string;
  matched_by: AccountDeduplicationMatch;
  identity_label: string;
  keep_id: string;
  keep_reason: "editable_physical_file" | "enabled_account" | "healthier_account" | "newer_evidence" | "more_complete_credential" | "deterministic_order";
  members: AccountDeduplicationMember[];
}

export interface AccountDeduplicationPreview {
  scanned_credentials: number;
  identified_credentials: number;
  excluded_credentials: number;
  duplicate_groups: number;
  duplicate_credentials: number;
  proposed_deletions: number;
  read_only_skipped: number;
  missing_identity: number;
  options: AccountDeduplicationOptions;
  groups: AccountDeduplicationGroup[];
}

export type ModelTestStatus = "available" | "unavailable" | "unsupported" | "review";

export interface ModelTestResult {
  account_id: string;
  provider: string;
  model: string;
  primary_model?: string;
  fallback_model?: string;
  selected_model?: string;
  fallback_used?: boolean;
  status: ModelTestStatus;
  probe_kind?: "model" | "credential";
  reason_code: string;
  status_code?: number;
  quota_window?: "five_hour" | "seven_day" | "multiple" | "five_hour_fallback";
  latency_ms: number;
  tested_at: string;
  response?: ModelTestResponsePreview;
  experiment?: ModelTestExperiment;
  attempts?: ModelTestAttempt[];
  compatible_models?: string[];
  model_policy?: {
    mode: "allow_only";
    models: string[];
    status: "applied" | "skipped" | "failed";
    reason_code: string;
  };
}

export interface ModelTestAttempt {
  model: string;
  role: "primary" | "fallback" | "compatibility";
  status: ModelTestStatus;
  probe_kind?: "model" | "credential";
  reason_code: string;
  status_code?: number;
  quota_window?: ModelTestResult["quota_window"];
  latency_ms: number;
  tested_at: string;
  response?: ModelTestResponsePreview;
  experiment?: ModelTestExperiment;
}

export interface ModelTestExperiment {
  name: "weekly_overdraft";
  applied: boolean;
  call_id?: string;
}

export interface ModelTestResponsePreview {
  format: "json" | "sse" | "text" | "empty";
  body: string;
  headers: ModelTestResponseHeader[];
  truncated: boolean;
}

export interface ModelTestResponseHeader {
  name: string;
  value: string;
}

export interface AccountDeleteTarget {
  id: string;
  name: string;
  provider?: string;
  type?: string;
  plan_type?: string;
  label?: string;
  email?: string;
  status?: string;
  source?: string;
}

export interface AccountDeletePreview {
  id: string;
  created_at: string;
  expires_at: string;
  account: AccountDeleteTarget;
}

export interface AccountDeleteResult {
  status: "deleted";
  deleted_at: string;
  account: AccountDeleteTarget;
}

export interface HeaderPatch {
  set?: Record<string, string>;
  remove?: string[];
}

export interface AccountPatch {
  disabled?: boolean;
  note?: string;
  prefix?: string;
  proxy_url?: string;
  websockets?: boolean;
  headers?: HeaderPatch;
	model_policy?: ModelPolicyPatch;
	concurrency_limit?: number;
}

export interface TargetScope {
  mode: "selected" | "filtered";
  ids?: string[];
  filters?: AccountFilters;
}

export interface PatchSummary {
  fields: string[];
  header_set?: string[];
  header_remove?: string[];
  proxy_mutation: boolean;
}

export interface PreviewTarget {
  id: string;
  name?: string;
  provider?: string;
  label?: string;
  eligible: boolean;
  read_only_reason?: string;
}

export interface ImportSkippedItem {
  source_name: string;
  source_path?: string;
  reason: string;
}

export interface ImportPreviewItem {
  index: number;
  source_name: string;
  source_path?: string;
  target_name: string;
  email?: string;
  account_id?: string;
  label: string;
  synthetic_id_token: boolean;
  warnings?: string[];
  credential_type?: "agent_identity" | "personal_access_token" | string;
}

export interface ImportPreview {
  id: string;
  created_at: string;
  expires_at: string;
  input_type: "json" | "text" | "zip" | "mixed";
  source_files: number;
  total: number;
  skipped: number;
  warnings?: string[];
  items: ImportPreviewItem[];
  skipped_items?: ImportSkippedItem[];
}

export interface ImportResultItem {
  index: number;
  source_name: string;
  source_path?: string;
  target_name: string;
  email?: string;
  account_id?: string;
  label: string;
  status: "imported" | "skipped" | "failed";
  error?: string;
}

export interface ImportResult {
  id: string;
  state: "idle" | "running" | "completed" | "partial" | "failed";
  running: boolean;
  total: number;
  imported: number;
  skipped: number;
  failed: number;
  started_at: string;
  finished_at: string;
  results: ImportResultItem[];
  error?: string;
}

export type AccountExportFormat = "cpa" | "sub2api" | "cockpit" | "9router" | "codex" | "axonhub" | "codexmanager";

export type ResultExportFormat = "json" | "csv" | "jsonl";

export type ExportFormat = AccountExportFormat | ResultExportFormat;

export type OperationCategory = "account" | "batch" | "import" | "export" | "default_policy" | "inspection" | "update" | "journal";
export type OperationStatus = "running" | "succeeded" | "partial" | "failed" | "interrupted" | "warning" | "skipped";
export type OperationSource = "manual" | "background" | "default_policy" | "inspection" | "import" | "plugin_store";
export type OperationExportFormat = "json" | "csv" | "jsonl";

export interface OperationFailureDetail {
  reason_code: string;
  count: number;
  sample_account_ids?: string[];
}

export interface OperationEntry {
  id: string;
  event_id?: string;
  category: OperationCategory;
  action: string;
  status: OperationStatus;
  source: OperationSource;
  scope?: string;
  target_id?: string;
  target_count: number;
  succeeded: number;
  failed: number;
  skipped: number;
  started_at: string;
  finished_at?: string;
  reason_code?: string;
  related_job_id?: string;
  related_action_id?: string;
  version?: string;
  format?: string;
  model?: string;
  http_status?: number;
  attempts?: number;
  failure_details?: OperationFailureDetail[];
}

export interface OperationSummary {
  total: number;
  running: number;
  succeeded: number;
  failed: number;
  attention: number;
  interrupted: number;
}

export interface OperationListResponse {
  operations: OperationEntry[];
  summary: OperationSummary;
  total: number;
  page: number;
  page_size: number;
  pages: number;
  extended_history: boolean;
  archived_segments: number;
  retention_limit: number;
  retained: number;
  storage_error?: string;
}

export interface OperationRetentionSettings {
  extended_history: boolean;
  page_size: number;
  retained: number;
  archived_segments: number;
}

export interface OperationFilters {
  category?: OperationCategory | "";
  status?: OperationStatus | "";
  source?: OperationSource | "";
  search?: string;
}

export interface Session {
  baseUrl: string;
  managementKey: string;
}

export interface UpdatePolicy {
  check_enabled: boolean;
  check_interval_hours: number;
  auto_update: boolean;
}

export interface UpdateSnapshot {
  policy: UpdatePolicy;
  current_version: string;
  latest_version?: string;
  update_available: boolean;
  release_url?: string;
  checking: boolean;
  pending: boolean;
  checked_at?: string;
  error?: string;
  release_source?: "plugin_store" | "none";
  store_error?: string;
  runtime?: {
    active: boolean;
    superseded: boolean;
    instance_version: string;
    owner_version?: string;
    process_scope?: string;
    storage_error?: string;
    restart_required: boolean;
    restart_recommended: boolean;
  };
}

export interface PluginStoreEntry {
  id: string;
  version: string;
  installed: boolean;
  installed_version: string;
  update_available: boolean;
}

export interface PluginStoreResponse {
  plugins_enabled: boolean;
  plugins: PluginStoreEntry[] | null;
}

export interface PluginInstallResult {
  status: "installed";
  id: string;
  version: string;
  restart_required: boolean;
}

export interface CPAServerVersionSnapshot {
  current_version?: string;
  latest_version?: string;
  current_build_date?: string;
  update_available: boolean;
  checked_at: string;
  release_url?: string;
  error?: "current_version_unavailable" | "latest_version_unavailable" | "version_comparison_unavailable";
}

export interface ExperimentalSettings {
  weekly_overdraft_enabled: boolean;
  agent_identity_enabled: boolean;
  auto_model_whitelist_enabled: boolean;
  sub2api_credit_usage_enabled: boolean;
}

export interface ExperimentalSettingsSnapshot {
  settings: ExperimentalSettings;
  storage_error?: string;
}

export interface AgentIdentitySessionLoginResponse {
  status: "completed";
  account: {
    email?: string;
    plan_type: string;
    provider: string;
    login_state: string;
  };
}


