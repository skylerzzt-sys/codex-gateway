import http from "node:http";

const port = Number(process.env.MOCK_CPA_PORT || 8318);
const managementKey = process.env.MOCK_CPA_KEY || "demo-key";
const defaultOpenAIProbeModel = "gpt-5.6-sol";
const previews = new Map();
const deletePreviews = new Map();
let activeJob = null;
const importPreviews = new Map();
let importJob = { id: "", state: "idle", running: false, total: 0, imported: 0, skipped: 0, failed: 0, results: [] };
let updatePolicy = {
  check_enabled: true,
  check_interval_hours: 24,
  auto_update: false,
};
let experimentalSettings = {
  weekly_overdraft_enabled: false,
  agent_identity_enabled: true,
};
let operationSettings = {
  extended_history: false,
};

const operationLog = Array.from({ length: 18 }, (_, index) => {
  const variants = [
    ["import", "import", "succeeded", "import"],
    ["export", "export_accounts", "succeeded", "manual"],
    ["update", "update_check", "warning", "background"],
  ];
  const [category, action, status, source] = variants[index % variants.length];
  const started = new Date(Date.now() - index * 47 * 60_000);
  return {
    id: `demo-operation-${index + 1}`,
    event_id: `demo-event-${index + 1}`,
    category,
    action,
    status,
    source,
    scope: category === "export" ? "filtered" : "system",
    target_count: 1,
    succeeded: status === "succeeded" ? 1 : 0,
    failed: status === "partial" ? 1 : 0,
    skipped: 0,
    started_at: started.toISOString(),
    finished_at: new Date(started.getTime() + 850).toISOString(),
    reason_code: status === "warning" ? "update_available" : status === "partial" ? "partial_failure" : "completed",
    version: category === "update" ? "0.3.0" : undefined,
    format: category === "export" ? "cpa" : undefined,
  };
});

const providers = ["codex", "claude", "gemini", "antigravity"];
const planTypes = ["free", "plus", "pro", "team", "business", "enterprise", "edu", "k12"];

const accounts = Array.from({ length: 36 }, (_, index) => {
  const provider = providers[index % providers.length];
  const readOnly = index % 11 === 0;
  const disabled = index % 7 === 0;
  const recentRequests = Array.from({ length: 6 }, (_, bucket) => ({
    time: new Date(Date.now() - (5 - bucket) * 10 * 60_000).toISOString(),
    success: (index + bucket * 2) % 7,
    failed: (index + bucket) % 9 === 0 ? 1 : 0,
  }));
  const hasUsage = index % 10 !== 9;
  const hasCodexQuota = provider === "codex" && index % 8 !== 0;
  const usage = hasUsage ? {
    input_tokens: 120_000 + index * 18_750,
    output_tokens: 34_000 + index * 4_200,
    reasoning_tokens: index % 3 === 0 ? 8_000 + index * 750 : 0,
    cached_tokens: index % 2 === 0 ? 21_000 + index * 600 : 0,
    cache_read_tokens: index % 2 === 0 ? 18_000 + index * 500 : 0,
    cache_creation_tokens: index % 5 === 0 ? 3_000 : 0,
    total_tokens: 162_000 + index * 23_700,
    last_request_at: new Date(Date.now() - (index % 7) * 7 * 60_000).toISOString(),
    updated_at: new Date(Date.now() - (index % 7) * 7 * 60_000).toISOString(),
    ...(hasCodexQuota ? {
      codex: {
        observed_at: new Date(Date.now() - 2 * 60_000).toISOString(),
				plan_type: planTypes[Math.floor(index / providers.length) % planTypes.length],
				active_reset_count: index % 12 === 4 ? 2 : 0,
				metadata_observed_at: new Date(Date.now() - 2 * 60_000).toISOString(),
        five_hour: {
          used_percent: index === 12 ? 118 : 18 + (index % 6) * 12.5,
          reset_at: new Date(Date.now() + 38 * 60_000).toISOString(),
          window_minutes: 300,
        },
        seven_day: {
          used_percent: 31 + (index % 5) * 14,
          reset_at: new Date(Date.now() + 4 * 24 * 60 * 60_000).toISOString(),
          window_minutes: 10_080,
        },
      },
    } : {}),
  } : undefined;
  return {
    id: `auth-${String(index + 1).padStart(3, "0")}`,
    auth_id: `runtime-${index + 1}`,
    name: `${provider}-${String(index + 1).padStart(2, "0")}.json`,
    provider,
    type: provider,
    label: `operator-${String(index + 1).padStart(2, "0")}@example.com`,
    email: `operator-${String(index + 1).padStart(2, "0")}@example.com`,
    account_type: index % 3 === 0 ? "oauth" : "api_key",
    plan_type: provider === "codex" ? planTypes[Math.floor(index / providers.length) % planTypes.length] : undefined,
    status: disabled ? "disabled" : index % 9 === 0 ? "error" : "active",
    status_message: index % 9 === 0 ? "upstream temporarily unavailable" : "",
    disabled,
    unavailable: index % 9 === 0,
    runtime_only: readOnly,
    source: readOnly ? "runtime" : "file",
    priority: (index % 8) - 2,
    note: index % 5 === 0 ? "primary pool" : "",
    prefix: index % 4 === 0 ? "team-a" : "",
    proxy: index % 6 === 0 ? "http://127.0.0.1:7890" : "",
    proxy_configured: index % 6 === 0,
    websockets: index % 3 === 0,
    header_names: index % 4 === 0 ? ["Authorization", "X-Team"] : [],
    header_count: index % 4 === 0 ? 2 : 0,
		model_policy: index % 5 === 0 ? { mode: "allow_only", models: ["gpt-5.5", "gpt-5.4-mini"], excluded_count: 2 } : undefined,
    editable: !readOnly,
    read_only_reason: readOnly ? "runtime-only account has no physical auth file" : "",
    success: 80 + index * 3,
    failed: index % 6,
    recent_requests: recentRequests,
    next_retry_after: index % 9 === 0 ? new Date(Date.now() + 12 * 60_000).toISOString() : undefined,
    ...(usage ? { usage } : {}),
		created_at: new Date(Date.now() - (45 + index) * 24 * 60 * 60_000).toISOString(),
		...(disabled ? { disabled_at: new Date(Date.now() - (index + 1) * 37 * 60_000).toISOString() } : {}),
    updated_at: new Date(Date.now() - index * 43 * 60_000).toISOString(),
  };
});

function mockUpdateSnapshot(pending = false) {
  return {
    policy: updatePolicy,
    current_version: "0.2.0",
    update_available: false,
    checking: false,
    pending,
    checked_at: new Date().toISOString(),
    error: "release metadata request failed",
  };
}

function json(response, status, body, headers = {}) {
  response.writeHead(status, { ...corsHeaders(), "Content-Type": "application/json; charset=utf-8", ...headers });
  response.end(JSON.stringify(body));
}

function textDownload(response, body, contentType, filename, headers = {}) {
  response.writeHead(200, {
    ...corsHeaders(),
    "Content-Type": contentType,
    "Content-Disposition": `attachment; filename="${filename}"`,
    "X-Content-Type-Options": "nosniff",
    ...headers,
  });
  response.end(body);
}

function corsHeaders() {
  return {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
    "Access-Control-Allow-Methods": "DELETE, GET, OPTIONS, PATCH, POST, PUT",
  };
}

function csvDocument(headers, rows) {
  const cell = (raw) => {
    let value = raw === undefined || raw === null ? "" : String(raw);
    if (/^[\s]*[=+\-@]/.test(value)) value = `'${value}`;
    return `"${value.replaceAll('"', '""')}"`;
  };
  return [headers, ...rows].map((row) => row.map(cell).join(",")).join("\n") + "\n";
}

const zipCrc32Table = (() => {
  const table = new Uint32Array(256);
  for (let index = 0; index < table.length; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) value = (value & 1) ? (0xedb88320 ^ (value >>> 1)) : (value >>> 1);
    table[index] = value >>> 0;
  }
  return table;
})();

function zipCrc32(bytes) {
  let crc = 0xffffffff;
  bytes.forEach((byte) => {
    crc = zipCrc32Table[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  });
  return (crc ^ 0xffffffff) >>> 0;
}

function createStoredZip(entries, now = new Date()) {
  const chunks = [];
  const centralDirectory = [];
  const year = Math.max(1980, now.getFullYear());
  const dosTime = (now.getHours() << 11) | (now.getMinutes() << 5) | Math.floor(now.getSeconds() / 2);
  const dosDate = ((year - 1980) << 9) | ((now.getMonth() + 1) << 5) | now.getDate();
  let offset = 0;

  entries.forEach((entry) => {
    const name = Buffer.from(entry.name, "utf8");
    const data = Buffer.from(entry.content, "utf8");
    const crc = zipCrc32(data);
    const local = Buffer.alloc(30 + name.length);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0x0800, 6);
    local.writeUInt16LE(0, 8);
    local.writeUInt16LE(dosTime, 10);
    local.writeUInt16LE(dosDate, 12);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(name.length, 26);
    name.copy(local, 30);

    const central = Buffer.alloc(46 + name.length);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0x0800, 8);
    central.writeUInt16LE(0, 10);
    central.writeUInt16LE(dosTime, 12);
    central.writeUInt16LE(dosDate, 14);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(offset, 42);
    name.copy(central, 46);

    chunks.push(local, data);
    centralDirectory.push(central);
    offset += local.length + data.length;
  });

  const centralSize = centralDirectory.reduce((size, entry) => size + entry.length, 0);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(centralSize, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...chunks, ...centralDirectory, end]);
}

function mockCPAAuth(account, index) {
  const base = {
    type: account.provider,
    name: account.label,
    email: account.email,
    disabled: account.disabled,
    priority: account.priority,
    note: account.note,
  };
  if (account.provider !== "codex") return { ...base, api_key: `demo-${account.provider}-key-${index + 1}` };
  return {
    ...base,
    type: "codex",
    account_id: `demo-account-${index + 1}`,
    chatgpt_account_id: `demo-account-${index + 1}`,
    plan_type: account.plan_type || (index % 2 ? "team" : "plus"),
    access_token: `demo-access-token-${index + 1}`,
    refresh_token: index % 3 ? `demo-refresh-token-${index + 1}` : "",
    id_token: `demo.id-token-${index + 1}.signature`,
    session_token: `demo-session-token-${index + 1}`,
    last_refresh: new Date().toISOString(),
    expired: new Date(Date.now() + 24 * 60 * 60_000).toISOString(),
  };
}

function mockCredentialRecord(account, index) {
  const cpa = mockCPAAuth(account, index);
  return {
    cpa,
    name: account.label,
    email: account.email,
    accountId: cpa.account_id,
    planType: cpa.plan_type,
    accessToken: cpa.access_token,
    refreshToken: cpa.refresh_token,
    idToken: cpa.id_token,
    expiresAt: cpa.expired,
    lastRefresh: cpa.last_refresh,
  };
}

function mockCredentialDocument(format, records) {
  const oneOrMany = (items) => items.length === 1 ? items[0] : items;
  if (format === "sub2api") {
    return {
      exported_at: new Date().toISOString(),
      proxies: [],
      accounts: records.map((record) => ({
        name: record.name,
        platform: "openai",
        type: "oauth",
        concurrency: 10,
        priority: 1,
        credentials: {
          access_token: record.accessToken,
          chatgpt_account_id: record.accountId,
          email: record.email,
          expires_at: record.refreshToken ? undefined : record.expiresAt,
          plan_type: record.planType,
        },
        extra: { email: record.email, name: record.name, source: "cpa", last_refresh: record.lastRefresh },
      })),
    };
  }
  if (format === "cockpit") return oneOrMany(records.map((record) => ({
    type: "codex", id_token: record.idToken, access_token: record.accessToken, refresh_token: record.refreshToken,
    account_id: record.accountId, last_refresh: record.lastRefresh, email: record.email, expired: record.expiresAt,
  })));
  if (format === "9router") return oneOrMany(records.map((record) => ({
    accessToken: record.accessToken, refreshToken: record.refreshToken, expiresAt: record.expiresAt,
    providerSpecificData: { chatgptAccountId: record.accountId, chatgptPlanType: record.planType },
    id: record.accountId, provider: "codex", authType: "oauth", name: record.name, email: record.email,
    priority: 9, isActive: true, createdAt: record.lastRefresh, updatedAt: record.lastRefresh, testStatus: "active",
  })));
  if (format === "codex") return oneOrMany(records.map((record) => ({
    auth_mode: "chatgpt", OPENAI_API_KEY: null,
    tokens: { id_token: record.idToken, access_token: record.accessToken, refresh_token: record.refreshToken, account_id: record.accountId },
    last_refresh: record.lastRefresh,
  })));
  if (format === "axonhub") return oneOrMany(records.map((record) => ({
    auth_mode: "chatgpt", last_refresh: record.lastRefresh,
    tokens: { access_token: record.accessToken, refresh_token: record.refreshToken || "__missing_refresh_token__", id_token: record.idToken },
    ...(record.refreshToken ? {} : { axonhub_refresh_token_placeholder: true }),
  })));
  return oneOrMany(records.map((record) => ({
    tokens: { access_token: record.accessToken, refresh_token: record.refreshToken, id_token: record.idToken, account_id: record.accountId, chatgpt_account_id: record.accountId },
    meta: { label: record.name, chatgpt_account_id: record.accountId, note: "Exported from CPA Account Config Manager" },
  })));
}

function mockCredentialStem(value, fallback) {
  const stem = String(value || "").trim().toLowerCase().replace(/[^a-z0-9@._+-]+/g, "-").replace(/^[.\-_]+|[.\-_]+$/g, "").slice(0, 96);
  return stem || fallback;
}

function mockCredentialHeaders(exported, skipped) {
  return {
    "Cache-Control": "no-store, private, max-age=0",
    Pragma: "no-cache",
    Expires: "0",
    "Referrer-Policy": "no-referrer",
    "X-Exported-Accounts": String(exported),
    "X-Skipped-Accounts": String(skipped),
    "Access-Control-Expose-Headers": "Content-Disposition, X-Exported-Accounts, X-Skipped-Accounts",
  };
}

function mockAccountCount(value) {
  if (Array.isArray(value)) return value.reduce((total, item) => total + mockAccountCount(item), 0);
  if (!value || typeof value !== "object") return 0;
  if (value.accessToken || value.access_token || value.tokens?.access_token || value.credentials?.access_token) return 1;
  return Object.values(value).reduce((total, item) => total + mockAccountCount(item), 0);
}

async function mockTextImportCount(file) {
  const content = (await file.text()).trim();
  if (!content) return 0;
  try {
    return Math.max(1, mockAccountCount(JSON.parse(content)));
  } catch {
    const documents = content.split(/\r?\n/).filter((line) => line.trim()).map((line) => JSON.parse(line));
    return documents.reduce((total, document) => total + Math.max(1, mockAccountCount(document)), 0);
  }
}

function mockImportType(file) {
  const name = file.name.toLowerCase();
  if (name.endsWith(".zip")) return "zip";
  if (name.endsWith(".txt") || name.endsWith(".jsonl") || name.endsWith(".ndjson")) return "text";
  return "json";
}

function authorized(request) {
  return request.headers.authorization === `Bearer ${managementKey}`;
}

async function readJSON(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return chunks.length ? JSON.parse(Buffer.concat(chunks).toString("utf8")) : {};
}

async function readFormData(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  const body = Buffer.concat(chunks);
  const webRequest = new Request("http://127.0.0.1/import", {
    method: "POST",
    headers: request.headers,
    body,
  });
  return webRequest.formData();
}

function filterAccounts(filters) {
  return accounts.filter((account) => {
    if (filters.provider && account.provider !== filters.provider) return false;
    if (filters.type && ![account.plan_type, account.account_type, account.type].includes(filters.type)) return false;
    if (filters.status && account.status !== filters.status) return false;
    if (filters.disabled !== undefined && account.disabled !== filters.disabled) return false;
    if (filters.editability === "editable" && !account.editable) return false;
    if ((filters.editability === "read_only" || filters.editability === "readonly") && account.editable) return false;
    if (filters.search) {
      const search = String(filters.search).toLowerCase();
      if (!`${account.id}\n${account.name}\n${account.label}\n${account.provider}\n${account.plan_type}\n${account.account_type}\n${account.note}`.toLowerCase().includes(search)) return false;
    }
    return true;
  });
}

function listFromURL(url) {
  const filters = {};
  for (const key of ["provider", "type", "status", "editability", "search"]) {
    if (url.searchParams.get(key)) filters[key] = url.searchParams.get(key);
  }
  if (url.searchParams.has("disabled")) filters.disabled = url.searchParams.get("disabled") === "true";
  const filtered = filterAccounts(filters);
  const page = Math.max(1, Number(url.searchParams.get("page") || 1));
  const pageSize = Math.max(1, Number(url.searchParams.get("page_size") || 50));
  return {
    accounts: filtered.slice((page - 1) * pageSize, page * pageSize),
    total: filtered.length,
    page,
    page_size: pageSize,
    pages: Math.ceil(filtered.length / pageSize),
  };
}

function resolveScope(scope) {
  if (scope.mode === "selected") {
    const ids = new Set(scope.ids || []);
    return accounts.filter((account) => ids.has(account.id));
  }
  return filterAccounts(scope.filters || {});
}

function snapshotJob(includeResults = true) {
  if (!activeJob) {
    return {
      state: "idle", running: false, total: 0, eligible: 0, done: 0, succeeded: 0,
      failed: 0, conflicts: 0, skipped: 0, workers: 0,
      patch: { fields: [], proxy_mutation: false }, retry_available: false, persisted: false,
    };
  }
  const elapsed = Date.now() - activeJob.started;
  const done = Math.min(activeJob.targets.length, Math.floor(elapsed / 260));
  const running = done < activeJob.targets.length;
  if (!running && activeJob.operation === "delete" && !activeJob.applied) {
    const deletedIDs = new Set(activeJob.targets.map((target) => target.id));
    for (let index = accounts.length - 1; index >= 0; index -= 1) {
      if (deletedIDs.has(accounts[index].id)) accounts.splice(index, 1);
    }
    activeJob.applied = true;
  }
  const results = activeJob.targets.map((target, index) => ({
    id: target.id,
    name: target.name,
    provider: target.provider,
    label: target.label,
    status: index < done ? "succeeded" : index === done && running ? "running" : "pending",
    applied_fields: index < done && activeJob.operation !== "delete" ? activeJob.fields : [],
    retryable: false,
  }));
  return {
    id: activeJob.id,
    operation: activeJob.operation || "patch",
    state: running ? "running" : "completed",
    running,
    total: activeJob.targets.length,
    eligible: activeJob.targets.length,
    done,
    succeeded: done,
    failed: 0,
    conflicts: 0,
    skipped: 0,
    workers: 6,
    patch: { fields: activeJob.fields, proxy_mutation: activeJob.fields.includes("proxy_url") },
    started_at: new Date(activeJob.started).toISOString(),
    finished_at: running ? undefined : new Date().toISOString(),
    retry_available: false,
    persisted: !running,
    ...(includeResults ? { results } : {}),
  };
}

function upsertMockOperation(eventID, operation) {
  const index = operationLog.findIndex((entry) => entry.event_id === eventID);
  const next = {
    id: index >= 0 ? operationLog[index].id : crypto.randomUUID(),
    event_id: eventID,
    target_count: 0,
    succeeded: 0,
    failed: 0,
    skipped: 0,
    started_at: new Date().toISOString(),
    ...operation,
  };
  if (index >= 0) operationLog[index] = next;
  else operationLog.push(next);
  if (operationLog.length > 2000) operationLog.splice(0, operationLog.length - 2000);
  return next;
}

function mockJobOperation(snapshot, action, category = "batch") {
  if (!snapshot.id) return;
  const status = snapshot.running ? "running" : snapshot.state === "completed" ? "succeeded" : snapshot.state;
  upsertMockOperation(`${category}:${snapshot.id}`, {
    category,
    action,
    status,
    source: "manual",
    scope: category === "batch" ? "selected" : "all",
    target_count: snapshot.total,
    succeeded: snapshot.succeeded,
    failed: snapshot.failed + snapshot.conflicts,
    skipped: snapshot.skipped,
    started_at: snapshot.started_at,
    finished_at: snapshot.finished_at,
    reason_code: snapshot.running ? undefined : status === "succeeded" ? "completed" : "partial_failure",
    related_job_id: snapshot.id,
  });
}

function filteredMockOperations(url) {
  const job = snapshotJob(false);
  mockJobOperation(job, job.operation === "delete" ? "batch_delete" : "batch_edit");
  const category = url.searchParams.get("category") || "";
  const status = url.searchParams.get("status") || "";
  const source = url.searchParams.get("source") || "";
  const search = (url.searchParams.get("search") || "").trim().toLowerCase();
  return operationLog.filter((operation) => {
    if (category && operation.category !== category) return false;
    if (status && operation.status !== status) return false;
    if (source && operation.source !== source) return false;
    if (!search) return true;
    return [operation.id, operation.category, operation.action, operation.status, operation.source, operation.scope, operation.target_id, operation.reason_code, operation.related_job_id, operation.version, operation.format, operation.model]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(search));
  }).sort((left, right) => new Date(right.finished_at || right.started_at) - new Date(left.finished_at || left.started_at));
}

function mockOperationSummary(operations) {
  return operations.reduce((summary, operation) => {
    summary.total += 1;
    if (operation.status === "running") summary.running += 1;
    else if (operation.status === "succeeded") summary.succeeded += 1;
    else if (operation.status === "failed") summary.failed += 1;
    else if (operation.status === "interrupted") summary.interrupted += 1;
    else summary.attention += 1;
    return summary;
  }, { total: 0, running: 0, succeeded: 0, failed: 0, attention: 0, interrupted: 0 });
}

function operationCSV(operations) {
  const headers = ["id", "category", "action", "status", "source", "scope", "target_id", "target_count", "succeeded", "failed", "skipped", "started_at", "finished_at", "reason_code", "related_job_id", "related_action_id", "version", "format", "model"];
  const rows = operations.map((entry) => headers.map((key) => entry[key]));
  return csvDocument(headers, rows);
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url || "/", `http://${request.headers.host}`);
  if (request.method === "OPTIONS") {
    response.writeHead(204, corsHeaders());
    response.end();
    return;
  }
  if (!authorized(request)) return json(response, 401, { error: "invalid management key" });

  if (request.method === "GET" && url.pathname.endsWith("/operations/settings")) {
    return json(response, 200, {
      extended_history: operationSettings.extended_history,
      page_size: 500,
      retained: Math.min(500, operationLog.length),
      archived_segments: 0,
      retention_limit: 500,
    });
  }
  if (request.method === "PUT" && url.pathname.endsWith("/operations/settings")) {
    const body = await readJSON(request);
    operationSettings = { extended_history: body.extended_history === true };
    return json(response, 200, {
      extended_history: operationSettings.extended_history,
      page_size: 500,
      retained: Math.min(500, operationLog.length),
      archived_segments: 0,
      retention_limit: 500,
    });
  }
  if (request.method === "GET" && url.pathname.endsWith("/operations/export")) {
    const format = url.searchParams.get("format") || "json";
    const operations = filteredMockOperations(url);
    const headers = { "X-Exported-Operations": String(operations.length), "Access-Control-Expose-Headers": "Content-Disposition, X-Exported-Operations" };
    if (format === "csv") return textDownload(response, operationCSV(operations), "text/csv; charset=utf-8", "demo-operations.csv", headers);
    if (format === "jsonl") return textDownload(response, operations.map((entry) => JSON.stringify(entry)).join("\n") + (operations.length ? "\n" : ""), "application/x-ndjson; charset=utf-8", "demo-operations.jsonl", headers);
    return textDownload(response, JSON.stringify({ exported_at: new Date().toISOString(), count: operations.length, operations }, null, 2) + "\n", "application/json; charset=utf-8", "demo-operations.json", headers);
  }
  if (request.method === "GET" && url.pathname.endsWith("/operations")) {
    const operations = filteredMockOperations(url);
    const page = Math.max(1, Number(url.searchParams.get("page")) || 1);
    const pageSize = Math.min(200, Math.max(1, Number(url.searchParams.get("page_size")) || 50));
    const start = (page - 1) * pageSize;
    return json(response, 200, {
      operations: operations.slice(start, start + pageSize),
      summary: mockOperationSummary(operations),
      total: operations.length,
      page,
      page_size: pageSize,
      pages: operations.length ? Math.ceil(operations.length / pageSize) : 0,
    });
  }
  if (request.method === "DELETE" && url.pathname.endsWith("/operations")) {
    operationLog.splice(0);
    const now = new Date().toISOString();
    const operation = upsertMockOperation(`journal-clear:${now}`, {
      category: "journal", action: "journal_clear", status: "succeeded", source: "manual", scope: "system",
      target_count: 0, succeeded: 0, failed: 0, skipped: 0, started_at: now, finished_at: now, reason_code: "completed",
    });
    return json(response, 200, { operation, retained: 1 });
  }
  if (request.method === "POST" && url.pathname.endsWith("/operations/record")) {
    const body = await readJSON(request);
    if (body.action !== "update_install" || !["succeeded", "failed", "warning"].includes(body.status)) return json(response, 400, { error: "unsupported operation record" });
    const now = new Date().toISOString();
    const operation = upsertMockOperation(`update-install:${crypto.randomUUID()}`, {
      category: "update", action: "update_install", status: body.status, source: "plugin_store", scope: "system",
      target_count: 0, succeeded: 0, failed: body.status === "failed" ? 1 : 0, skipped: 0, started_at: now, finished_at: now,
      reason_code: body.status === "warning" ? "restart_required" : body.status === "failed" ? "install_failed" : "completed", version: body.version,
    });
    return json(response, 201, operation);
  }

  if (request.method === "GET" && url.pathname.endsWith("/plugins/cpa-account-config-manager/accounts")) {
    return json(response, 200, listFromURL(url));
  }
	if (request.method === "POST" && url.pathname.endsWith("/accounts/quota-metadata/refresh")) {
		const body = await readJSON(request);
		const account = accounts.find((candidate) => candidate.id === body.account_id);
		if (!account) return json(response, 404, { error: "quota metadata account was not found" });
		if (!String(account.provider).startsWith("codex")) return json(response, 422, { error: "quota metadata is only available for Codex accounts" });
		account.usage ||= { input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, cached_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, total_tokens: 0 };
		account.usage.codex ||= { observed_at: new Date().toISOString() };
		account.usage.codex.plan_type = account.plan_type || "free";
		account.usage.codex.active_reset_count ??= 0;
		account.usage.codex.metadata_observed_at = new Date().toISOString();
		return json(response, 200, { account_id: account.id, plan_type: account.usage.codex.plan_type, active_reset_count: account.usage.codex.active_reset_count, observed_at: account.usage.codex.metadata_observed_at });
	}
	if (request.method === "POST" && url.pathname.endsWith("/accounts/quota-metadata/reset")) {
		const body = await readJSON(request);
		const account = accounts.find((candidate) => candidate.id === body.account_id);
		if (!account) return json(response, 404, { error: "quota metadata account was not found" });
		if (body.confirm !== true) return json(response, 400, { error: "active reset confirmation is required" });
		const count = account.usage?.codex?.active_reset_count;
		if (!Number.isInteger(count) || count <= 0) return json(response, 409, { error: "no active reset credit is available" });
		account.usage.codex.active_reset_count = count - 1;
		account.usage.codex.metadata_observed_at = new Date().toISOString();
		return json(response, 200, { account_id: account.id, plan_type: account.usage.codex.plan_type || account.plan_type, active_reset_count: account.usage.codex.active_reset_count, observed_at: account.usage.codex.metadata_observed_at, reset_credit_used: true });
	}
  if (request.method === "POST" && url.pathname.endsWith("/accounts/deduplicate/preview")) {
    const requested = await readJSON(request);
    const options = {
      ignore_account_id: Boolean(requested.ignore_account_id),
      exclude_team_accounts: Boolean(requested.exclude_team_accounts),
    };
    const member = (account, recommendedAction) => ({
      id: account.id,
      name: account.name,
      email: account.email,
      provider: account.provider,
      type: account.type,
      plan_type: account.plan_type,
      status: account.status,
      disabled: account.disabled,
      unavailable: account.unavailable,
      editable: account.editable,
      read_only_reason: account.read_only_reason,
      updated_at: account.updated_at,
      recommended_action: recommendedAction,
    });
    return json(response, 200, {
      scanned_credentials: accounts.length,
      identified_credentials: accounts.length - 1 - (options.exclude_team_accounts ? 2 : 0),
      excluded_credentials: options.exclude_team_accounts ? 2 : 0,
      duplicate_groups: 2,
      duplicate_credentials: 3,
      proposed_deletions: 2,
      read_only_skipped: 1,
      missing_identity: 1,
      options,
      groups: [
        {
          id: "mock-codex-identity",
          provider: "codex",
          matched_by: "account_id",
          identity_label: "ID #8ad93f20b417",
          keep_id: accounts[4].id,
          keep_reason: "healthier_account",
          members: [member(accounts[4], "keep"), member(accounts[8], "delete"), member(accounts[0], "skip")],
        },
        {
          id: "mock-codex-email",
          provider: "codex",
          matched_by: "email",
          identity_label: "shared-codex@example.com",
          keep_id: accounts[12].id,
          keep_reason: "newer_evidence",
          members: [member(accounts[12], "keep"), member(accounts[16], "delete")],
        },
      ],
    });
  }
  if (request.method === "POST" && url.pathname.endsWith("/experiments/agent-identity/session-login")) {
    const body = await readJSON(request);
    const state = typeof body.state === "string" ? body.state.trim() : "";
    const sessionJSON = typeof body.session_json === "string" ? body.session_json.trim() : "";
    if (!/^[A-Za-z0-9._-]{1,256}$/.test(state) || sessionJSON === "") return json(response, 400, { error: "invalid_session" });
    try {
      JSON.parse(sessionJSON);
    } catch {
      return json(response, 400, { error: "invalid_session" });
    }
    return json(response, 200, {
      status: "completed",
      account: { email: "agent@example.com", plan_type: "team", provider: "codex-agent-identity", login_state: state },
    });
  }
  if (request.method === "GET" && url.pathname.endsWith("/experiments")) {
    return json(response, 200, { settings: experimentalSettings });
  }
  if (request.method === "PUT" && url.pathname.endsWith("/experiments")) {
    const body = await readJSON(request);
    experimentalSettings = {
      weekly_overdraft_enabled: Boolean(body.weekly_overdraft_enabled),
      agent_identity_enabled: Boolean(body.agent_identity_enabled),
    };
    return json(response, 200, { settings: experimentalSettings });
  }
	if (request.method === "POST" && url.pathname.endsWith("/accounts/config")) {
		const body = await readJSON(request);
		const account = accounts.find((candidate) => candidate.id === body.account_id);
		if (!account) return json(response, 404, { error: "account was not found" });
		if (!account.editable) return json(response, 409, { error: "account is read-only" });
		return json(response, 200, {
			account_id: account.id,
			disabled: account.disabled,
			priority: account.priority ?? null,
			note: account.note || "",
			prefix: account.prefix || "",
			proxy: account.proxy || "",
			proxy_configured: account.proxy_configured,
			websockets: account.websockets ?? null,
			header_names: account.header_names || [],
			model_policy: account.model_policy || null,
		});
	}
  if (request.method === "POST" && url.pathname.endsWith("/accounts/models")) {
    const body = await readJSON(request);
    const targets = resolveScope(body.scope || {});
    const editable = targets.filter((target) => target.editable);
    const provider = editable[0]?.provider || "";
    const sameProvider = editable.every((target) => target.provider === provider);
    const byProvider = {
      codex: ["gpt-5.4", "gpt-5.5", "gpt-5.6-sol"],
      "codex-agent-identity": ["gpt-5.5", "gpt-5.6-sol"],
      claude: ["claude-sonnet-4-5-20250929", "claude-opus-4-1"],
      gemini: ["gemini-2.0-flash", "gemini-2.5-pro"],
      antigravity: ["gemini-2.0-flash", "gemini-2.5-pro"],
    };
    const models = sameProvider ? (byProvider[provider] || []).map((id) => ({ id, display_name: id.toUpperCase(), owned_by: provider })) : [];
    return json(response, 200, {
      models,
      ...(editable.length === 1 ? { current_policy: editable[0].model_policy || { mode: "all", excluded_count: 0 } } : {}),
      total: targets.length,
      eligible: editable.length,
      loaded: editable.length,
      failed: 0,
      read_only: targets.length - editable.length,
      missing: 0,
      warnings: [],
    });
  }
  if (request.method === "POST" && url.pathname.endsWith("/accounts/model-test")) {
    const body = await readJSON(request);
    const account = accounts.find((candidate) => candidate.id === body.account_id);
    if (!account) return json(response, 404, { error: "account was not found" });
    const defaults = { codex: defaultOpenAIProbeModel, openai: defaultOpenAIProbeModel, claude: "claude-sonnet-4-5-20250929", gemini: "gemini-2.0-flash", aistudio: "gemini-2.0-flash", xai: "grok-4" };
    const model = String(body.model || defaults[account.provider] || "").trim();
    const supported = ["codex", "codex-agent-identity", "openai", "claude", "gemini", "gemini-cli", "gemini-interactions", "aistudio", "xai"].includes(account.provider);
    const now = new Date().toISOString();
    const experimentalOverdraft = Boolean(body.experimental_weekly_overdraft) && ["codex", "codex-agent-identity"].includes(account.provider);
    const usesFallback = !experimentalOverdraft && supported && ["codex", "codex-agent-identity"].includes(account.provider) && model === defaultOpenAIProbeModel;
    const selectedModel = usesFallback ? "gpt-5.5" : model;
    const result = experimentalOverdraft ? {
      account_id: account.id,
      provider: account.provider,
      model,
      primary_model: model,
      status: "review",
      probe_kind: "model",
      reason_code: "quota_limited",
      status_code: 429,
      latency_ms: 466,
      tested_at: now,
      experiment: { name: "weekly_overdraft", applied: true, call_id: "call_cpa_overdraft_mock429" },
      response: {
        format: "json",
        body: "{\n  &#34;error&#34;: {\n    &#34;_omitted_fields&#34;: 4,\n    &#34;message&#34;: &#34;The usage limit has been reached&#34;,\n    &#34;type&#34;: &#34;usage_limit_reached&#34;\n  }\n}",
        headers: [
          { name: "cf-ray", value: "a1f9ebf56c42e3c4-IAD" },
          { name: "content-type", value: "application/json" },
        ],
        truncated: false,
      },
    } : {
      account_id: account.id,
      provider: account.provider,
      model: selectedModel,
      primary_model: model,
      ...(usesFallback ? { fallback_model: selectedModel, selected_model: selectedModel, fallback_used: true } : supported ? { selected_model: selectedModel } : {}),
      status: supported ? "available" : "unsupported",
      probe_kind: supported ? "model" : undefined,
      reason_code: supported ? "model_response_ok" : "unsupported_provider",
      status_code: supported ? 200 : undefined,
      latency_ms: usesFallback ? 604 : supported ? 286 : 0,
      tested_at: now,
      ...(usesFallback ? {
        attempts: [
          {
            model, role: "primary", status: "unavailable", probe_kind: "model", reason_code: "model_not_found",
            status_code: 400, latency_ms: 248, tested_at: now,
            response: {
              format: "json",
              body: JSON.stringify({ detail: `The '${model}' model is not supported when using Codex with a ChatGPT account.` }, null, 2),
              headers: [{ name: "content-type", value: "application/json" }], truncated: false,
            },
          },
          {
            model: selectedModel, role: "fallback", status: "available", probe_kind: "model", reason_code: "model_response_ok",
            status_code: 200, latency_ms: 356, tested_at: now,
            response: {
              format: "sse",
              body: "event: response.completed\ndata:\n{\n  \"type\": \"response.completed\",\n  \"response\": {\n    \"output\": [\n      {\n        \"content\": [\n          {\n            \"type\": \"output_text\",\n            \"text\": \"OK\"\n          }\n        ],\n        \"type\": \"message\"\n      }\n    ],\n    \"status\": \"completed\"\n  }\n}",
              headers: [{ name: "content-type", value: "text/event-stream" }], truncated: false,
            },
          },
        ],
      } : {}),
    };
    upsertMockOperation(`model-test:${crypto.randomUUID()}`, {
      category: "account", action: "model_test", status: supported ? "succeeded" : "skipped", source: "manual", scope: "single",
      target_id: account.id, target_count: 1, succeeded: supported ? 1 : 0, failed: 0, skipped: supported ? 0 : 1,
      started_at: now, finished_at: now, reason_code: result.reason_code, model: selectedModel,
    });
    return json(response, 200, result);
  }
  if (request.method === "POST" && url.pathname.endsWith("/accounts/delete/preview")) {
    const body = await readJSON(request);
    const account = accounts.find((candidate) => candidate.id === body.id);
    if (!account) return json(response, 404, { error: "account was not found" });
    if (!account.editable || account.runtime_only || account.source !== "file") {
      return json(response, 400, { error: "account is read-only and cannot be deleted" });
    }
    const previewID = crypto.randomUUID();
    const preview = {
      id: previewID,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      account: {
        id: account.id,
        name: account.name,
        provider: account.provider,
        type: account.type,
        plan_type: account.plan_type,
        label: account.label,
        email: account.email,
        status: account.status,
        source: account.source,
      },
    };
    deletePreviews.set(previewID, { accountID: account.id, name: account.name, preview });
    return json(response, 200, preview);
  }
  if (request.method === "POST" && url.pathname.endsWith("/accounts/delete/start")) {
    const body = await readJSON(request);
    const stored = deletePreviews.get(body.preview_id);
    if (!stored) return json(response, 404, { error: "delete preview not found" });
    const accountIndex = accounts.findIndex((candidate) => candidate.id === stored.accountID && candidate.name === stored.name);
    if (accountIndex < 0 || !accounts[accountIndex].editable) {
      return json(response, 409, { error: "account changed after delete preview" });
    }
    accounts.splice(accountIndex, 1);
    deletePreviews.delete(body.preview_id);
    return json(response, 200, {
      status: "deleted",
      deleted_at: new Date().toISOString(),
      account: stored.preview.account,
    });
  }
  if (request.method === "POST" && url.pathname.endsWith("/batch/delete/preview")) {
    const body = await readJSON(request);
    const targets = resolveScope(body.scope || {});
    const editable = targets.filter((target) => target.editable);
    const previewID = crypto.randomUUID();
    const preview = {
      operation: "delete",
      id: previewID,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      scope_mode: body.scope?.mode || "filtered",
      total: targets.length,
      eligible: editable.length,
      read_only: targets.length - editable.length,
      missing: 0,
      physical_files: editable.length,
      providers: Object.fromEntries(providers.map((provider) => [provider, targets.filter((target) => target.provider === provider).length]).filter(([, count]) => count > 0)),
      patch: { fields: [], proxy_mutation: false },
      warnings: targets.some((target) => !target.editable) ? [`${targets.length - editable.length} target(s) are read-only and will be skipped`] : [],
      targets: targets.map((target) => ({ id: target.id, name: target.name, provider: target.provider, label: target.label, eligible: target.editable, read_only_reason: target.read_only_reason })),
    };
    previews.set(previewID, { preview, targets: editable, fields: [], operation: "delete" });
    return json(response, 200, preview);
  }
  if (request.method === "POST" && url.pathname.endsWith("/batch/delete/start")) {
    const body = await readJSON(request);
    if (body.confirm !== true) return json(response, 400, { error: "batch deletion requires explicit confirmation" });
    const stored = previews.get(body.preview_id);
    if (!stored || stored.operation !== "delete") return json(response, 404, { error: "delete preview not found" });
    activeJob = { id: crypto.randomUUID(), started: Date.now(), targets: stored.targets, fields: [], operation: "delete", applied: false };
    return json(response, 202, snapshotJob(true));
  }
  if (request.method === "POST" && url.pathname.endsWith("/batch/preview")) {
    const body = await readJSON(request);
    const targets = resolveScope(body.scope || {});
    const fields = Object.keys(body.patch || {});
    const previewID = crypto.randomUUID();
    const preview = {
      operation: "patch",
      id: previewID,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      scope_mode: body.scope?.mode || "filtered",
      total: targets.length,
      eligible: targets.filter((target) => target.editable).length,
      read_only: targets.filter((target) => !target.editable).length,
      missing: 0,
      physical_files: targets.filter((target) => target.editable).length,
      providers: Object.fromEntries(providers.map((provider) => [provider, targets.filter((target) => target.provider === provider).length]).filter(([, count]) => count > 0)),
      patch: {
        fields,
        header_set: Object.keys(body.patch?.headers?.set || {}),
        header_remove: body.patch?.headers?.remove || [],
        proxy_mutation: fields.includes("proxy_url"),
      },
      warnings: targets.some((target) => !target.editable) ? [`${targets.filter((target) => !target.editable).length} target(s) are read-only and will be skipped`] : [],
      targets: targets.map((target) => ({ id: target.id, name: target.name, provider: target.provider, label: target.label, eligible: target.editable, read_only_reason: target.read_only_reason })),
    };
    previews.set(previewID, { preview, targets: targets.filter((target) => target.editable), fields, operation: "patch" });
    return json(response, 200, preview);
  }
  if (request.method === "POST" && url.pathname.endsWith("/batch/start")) {
    const body = await readJSON(request);
    const stored = previews.get(body.preview_id);
    if (!stored) return json(response, 404, { error: "preview not found" });
    activeJob = { id: crypto.randomUUID(), started: Date.now(), targets: stored.targets, fields: stored.fields, operation: "patch" };
    return json(response, 202, snapshotJob(true));
  }
  if (request.method === "GET" && url.pathname.endsWith("/batch/status")) {
    return json(response, 200, snapshotJob(url.searchParams.get("light") !== "1"));
  }
  if (request.method === "POST" && url.pathname.endsWith("/batch/retry")) {
    return json(response, 400, { error: "no failed targets are available to retry" });
  }
  if ((request.method === "GET" || request.method === "POST") && url.pathname.endsWith("/export/accounts")) {
    const format = url.searchParams.get("format") || "";
    let view = listFromURL(url);
    if (request.method === "POST") {
      const body = await readJSON(request);
      if (body.scope?.mode !== "selected" || !Array.isArray(body.scope.ids) || body.scope.ids.length === 0) {
        return json(response, 400, { error: "selected scope requires at least one account id" });
      }
      const ids = new Set(body.scope.ids);
      view = { accounts: accounts.filter((account) => ids.has(account.id)), total: ids.size };
    }
    const supported = new Set(["cpa", "sub2api", "cockpit", "9router", "codex", "axonhub", "codexmanager"]);
    if (!supported.has(format)) return json(response, 400, { error: "请选择账号导出目标格式" });
    const fileAccounts = view.accounts.filter((account) => !account.runtime_only && account.source === "file");
    if (format === "cpa") {
      if (!fileAccounts.length) return json(response, 422, { error: "当前筛选没有可导出的文件账号" });
      const documents = fileAccounts.map((account, index) => ({ account, content: JSON.stringify(mockCPAAuth(account, index), null, 2) + "\n" }));
      const headers = mockCredentialHeaders(documents.length, view.accounts.length - documents.length);
      if (documents.length === 1) {
        return textDownload(response, documents[0].content, "application/json; charset=utf-8", `${mockCredentialStem(documents[0].account.email, "account-001")}.json`, headers);
      }
      const used = new Set();
      const entries = documents.map(({ account, content }, index) => {
        const stem = mockCredentialStem(account.email, `account-${String(index + 1).padStart(3, "0")}`);
        let candidate = stem;
        let suffix = 1;
        while (used.has(`${candidate}.json`)) {
          suffix += 1;
          candidate = `${stem}-${suffix}`;
        }
        used.add(`${candidate}.json`);
        return { name: `${candidate}.json`, content };
      });
      return textDownload(response, createStoredZip(entries), "application/zip", "cpa-accounts.zip", headers);
    }
    const compatible = fileAccounts.filter((account) => account.provider === "codex");
    if (!compatible.length) return json(response, 422, { error: "当前筛选没有兼容的 Codex OAuth 账号" });
    const records = compatible.map(mockCredentialRecord);
    const body = JSON.stringify(mockCredentialDocument(format, records), null, 2) + "\n";
    const suffix = format === "codexmanager" ? "codex-manager" : format;
    const filename = format === "codex" && records.length === 1 ? "auth.json" : `cpa-accounts.${suffix}.json`;
    return textDownload(response, body, "application/json; charset=utf-8", filename, mockCredentialHeaders(records.length, view.accounts.length - records.length));
  }
  if (request.method === "GET" && url.pathname.endsWith("/export/results")) {
    const format = url.searchParams.get("format") || "json";
    const snapshot = snapshotJob(true);
    const results = snapshot.results || [];
    if (format === "csv") {
      const headers = ["job_id", "job_state", "id", "name", "provider", "label", "status", "error", "applied_fields", "retryable"];
      const rows = results.map((result) => [snapshot.id, snapshot.state, result.id, result.name, result.provider, result.label, result.status, result.error, result.applied_fields?.join(";"), result.retryable]);
      return textDownload(response, csvDocument(headers, rows), "text/csv; charset=utf-8", "demo-results.csv");
    }
    if (format === "jsonl" || format === "ndjson") {
      const body = results.map((result) => JSON.stringify({ job_id: snapshot.id, job_state: snapshot.state, ...result })).join("\n") + (results.length ? "\n" : "");
      return textDownload(response, body, "application/x-ndjson; charset=utf-8", "demo-results.jsonl");
    }
    return json(response, 200, snapshot, { "Content-Disposition": 'attachment; filename="demo-results.json"', "X-Content-Type-Options": "nosniff" });
  }
  if (request.method === "POST" && url.pathname.endsWith("/import/preview")) {
    const formData = await readFormData(request);
    const files = formData.getAll("files").filter((file) => typeof file?.name === "string");
    if (!files.length) return json(response, 400, { error: "multipart import contains no files" });
    const previewID = crypto.randomUUID();
    const items = [];
    let sourceFiles = 0;
    for (const [fileIndex, file] of files.entries()) {
      const isZip = file.name.toLowerCase().endsWith(".zip");
      const count = isZip ? 2 : await mockTextImportCount(file);
      sourceFiles += isZip ? count : 1;
      for (let entryIndex = 0; entryIndex < count; entryIndex += 1) {
        const sequence = items.length + 1;
        const stem = file.name.replace(/\.[^.]+$/, "").replace(/[^a-z0-9]+/gi, "_").toLowerCase() || `import_${sequence}`;
        const email = `${stem}_${entryIndex + 1}@example.com`;
        items.push({
          index: sequence,
          source_name: isZip ? `${file.name}/account-${entryIndex + 1}.json` : file.name,
          source_path: isZip ? `$[${entryIndex}]` : count > 1 ? `$document[${entryIndex}]` : "$",
          target_name: `codex-${stem}_${entryIndex + 1}.json`,
          email,
          account_id: `demo-import-${fileIndex + 1}-${entryIndex + 1}`,
          label: email,
          synthetic_id_token: true,
          warnings: ["ID token was synthesized from account metadata"],
        });
      }
    }
    const inputTypes = new Set(files.map(mockImportType));
    const preview = {
      id: previewID,
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      input_type: inputTypes.size > 1 ? "mixed" : [...inputTypes][0],
      source_files: sourceFiles,
      total: items.length,
      skipped: 0,
      warnings: ["existing Auth files will not be overwritten"],
      items,
    };
    importPreviews.set(previewID, preview);
    return json(response, 200, preview);
  }
  if (request.method === "POST" && url.pathname.endsWith("/import/start")) {
    const body = await readJSON(request);
    const preview = importPreviews.get(body.preview_id);
    if (!preview) return json(response, 404, { error: "import preview not found" });
    const results = preview.items.map((item) => {
      accounts.push({
        id: `auth-import-${crypto.randomUUID()}`,
        auth_id: `runtime-${item.account_id}`,
        name: item.target_name,
        provider: "codex",
        type: "codex",
        label: item.label,
        email: item.email,
        account_type: "oauth",
        plan_type: "k12",
        status: "active",
        status_message: "",
        disabled: false,
        unavailable: false,
        runtime_only: false,
        source: "file",
        priority: 0,
        note: "Imported by mock CPA",
        prefix: "",
        proxy: "",
        proxy_configured: false,
        websockets: false,
        header_names: [],
        header_count: 0,
        editable: true,
        read_only_reason: "",
        success: 0,
        failed: 0,
        updated_at: new Date().toISOString(),
      });
      return { ...item, status: "imported" };
    });
    importPreviews.delete(body.preview_id);
    importJob = {
      id: preview.id,
      state: "completed",
      running: false,
      total: results.length,
      imported: results.length,
      skipped: 0,
      failed: 0,
      started_at: new Date().toISOString(),
      finished_at: new Date().toISOString(),
      results,
    };
    return json(response, 202, { ...importJob, state: "running", running: true, imported: 0, results: [], finished_at: "0001-01-01T00:00:00Z" });
  }
  if (request.method === "GET" && url.pathname.endsWith("/import/status")) {
    return json(response, 200, importJob);
  }
  if (request.method === "PATCH" && url.pathname.endsWith("/plugins/cpa-account-config-manager/config")) {
    return json(response, 200, { status: "ok" });
  }
  if (request.method === "GET" && url.pathname === "/v0/management/latest-version") {
    return json(response, 200, { "latest-version": "v7.2.93" }, {
      "X-CPA-Version": "v7.2.92",
      "X-CPA-Build-Date": "2026-07-20T08:00:00Z",
    });
  }
  if (request.method === "POST" && url.pathname.endsWith("/updates/check")) {
    return json(response, 202, mockUpdateSnapshot(true));
  }
  if (request.method === "GET" && url.pathname.endsWith("/updates")) {
    return json(response, 200, mockUpdateSnapshot());
  }
  if (request.method === "GET" && url.pathname === "/v0/management/plugin-store") {
    return json(response, 200, {
      plugins_enabled: true,
      plugins: [{ id: "cpa-account-config-manager", version: "0.3.0", installed: true, installed_version: "0.2.0", update_available: true }],
    });
  }
  if (request.method === "POST" && url.pathname === "/v0/management/plugin-store/cpa-account-config-manager/install") {
    return json(response, 200, { status: "installed", id: "cpa-account-config-manager", version: "0.3.0", restart_required: true });
  }
  return json(response, 404, { error: "not found" });
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`Mock CPA listening on http://127.0.0.1:${port} (key: ${managementKey})\n`);
});
