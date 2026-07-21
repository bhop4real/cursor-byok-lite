import {
  GetState,
  LoadUserConfig,
  SaveUserConfig,
  StartProxy,
  StopProxy,
} from "@bindings/cursor/internal/bridge/proxyservice.js";
import { GetHomeMetricsSummary } from "@bindings/cursor/internal/bridge/metricsservice.js";
import {
  CheckForUpdates,
  GetAppVersion,
  InstallReadyUpdate,
  GetModelEditorContext,
  OpenConfigWindow,
  OpenHistoryWindow,
  OpenModelConfigWindow,
  OpenModelEditorWindow,
} from "@bindings/cursor/internal/bridge/windowservice.js";
import { Call } from "@wailsio/runtime";

const API_LOG_PREFIX = "[clientApi]";
const PROXY_SERVICE_NAME = "cursor/internal/bridge.ProxyService";
const REDACTED_LOG_VALUE = "[redacted]";

function isSensitiveLogKey(key) {
  const normalized = String(key || "").replace(/[^a-z0-9]/gi, "").toLowerCase();
  return normalized === "key"
    || normalized === "apikey"
    || normalized.endsWith("apikey")
    || normalized.endsWith("token")
    || normalized.includes("password")
    || normalized.includes("secret")
    || normalized.includes("credential")
    || normalized === "authorization"
    || normalized === "proxyauthorization"
    || normalized === "cookie"
    || normalized === "setcookie"
    || normalized === "adapterjson"
    || normalized === "customheadersjson";
}

function sanitizeLogValue(value, seen = new WeakSet()) {
  if (value === null || value === undefined || typeof value !== "object") {
    return value;
  }
  if (value instanceof Error) {
    return {
      name: value.name,
      message: value.message,
    };
  }
  if (seen.has(value)) {
    return "[circular]";
  }
  seen.add(value);
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeLogValue(item, seen));
  }
  const sanitized = {};
  for (const [key, item] of Object.entries(value)) {
    sanitized[key] = isSensitiveLogKey(key)
      ? REDACTED_LOG_VALUE
      : sanitizeLogValue(item, seen);
  }
  return sanitized;
}

function logSuccess(name, payload, result) {
  console.log(`${API_LOG_PREFIX} ${name} response`, {
    payload: sanitizeLogValue(payload),
    result: sanitizeLogValue(result),
  });
}

function logError(name, payload, error) {
  console.error(`${API_LOG_PREFIX} ${name} error`, {
    payload: sanitizeLogValue(payload),
    error: sanitizeLogValue(error),
  });
}

function withApiLogging(name, payload, runner) {
  return Promise.resolve()
    .then(() => runner())
    .then((result) => {
      logSuccess(name, payload, result);
      return result;
    })
    .catch((error) => {
      logError(name, payload, error);
      throw error;
    });
}

export function loadUserConfig() {
  return withApiLogging("LoadUserConfig", undefined, () => LoadUserConfig());
}

export function saveUserConfig(payload) {
  return withApiLogging("SaveUserConfig", payload, () => SaveUserConfig(payload));
}

export function getEditableConfigMetadata() {
  return withApiLogging("GetEditableConfigMetadata", undefined, () =>
    Call.ByName(`${PROXY_SERVICE_NAME}.GetEditableConfigMetadata`),
  );
}

export function patchUserConfig(payload) {
  return withApiLogging("PatchUserConfig", payload, () =>
    Call.ByName(`${PROXY_SERVICE_NAME}.PatchUserConfig`, payload),
  );
}

export function getProxyState() {
  return withApiLogging("GetState", undefined, () => GetState());
}

export function getHomeMetricsSummary() {
  return withApiLogging("GetHomeMetricsSummary", undefined, () => GetHomeMetricsSummary());
}

export function startProxyService() {
  return withApiLogging("StartProxy", undefined, () => StartProxy());
}

export function stopProxyService() {
  return withApiLogging("StopProxy", undefined, () => StopProxy());
}

export function openLogsDirectory() {
  return withApiLogging("OpenHistoryWindow", undefined, () => OpenHistoryWindow());
}

export function openConfigWindow() {
  return withApiLogging("OpenConfigWindow", undefined, () => OpenConfigWindow());
}

export function getAppVersion() {
  return withApiLogging("GetAppVersion", undefined, () => GetAppVersion());
}

export function checkForUpdates() {
  return withApiLogging("CheckForUpdates", undefined, () => CheckForUpdates());
}

export function installReadyUpdate() {
  return withApiLogging("InstallReadyUpdate", undefined, () => InstallReadyUpdate());
}

export function openModelConfig() {
  return withApiLogging("OpenModelConfigWindow", undefined, () => OpenModelConfigWindow());
}

export function openModelEditor(index, adapterJSON) {
  return withApiLogging("OpenModelEditorWindow", { index, adapterJSON }, () =>
    OpenModelEditorWindow(index, adapterJSON),
  );
}

export function getModelEditorContext() {
  return withApiLogging("GetModelEditorContext", undefined, () => GetModelEditorContext());
}

export function testModelAdapter(adapter) {
  return Call.ByName(`${PROXY_SERVICE_NAME}.TestModelAdapter`, adapter).then(
    (result) => {
      logSuccess("TestModelAdapter", adapter, result);
      return result;
    },
    (error) => {
      logError("TestModelAdapter", adapter, error);
      throw error;
    },
  );
}

export function getModelAdapterTestResults() {
  return withApiLogging("GetModelAdapterTestResults", undefined, () =>
    Call.ByName(`${PROXY_SERVICE_NAME}.GetModelAdapterTestResults`),
  );
}

const WINDOW_SERVICE_NAME = "cursor/internal/bridge.WindowService";
const PROFILER_SERVICE_NAME = "cursor/internal/bridge.ProfilerService";

export function openSettingsDirectory() {
  return withApiLogging("OpenSettingsDirectory", undefined, () =>
    Call.ByName(`${WINDOW_SERVICE_NAME}.OpenSettingsDirectory`),
  );
}

export function getProfilerStatus() {
  return withApiLogging("GetProfilerStatus", undefined, () =>
    Call.ByName(`${PROFILER_SERVICE_NAME}.GetProfilerStatus`),
  );
}

export function startProfiling(traceLimitSeconds) {
  return withApiLogging("StartProfiling", { traceLimitSeconds }, () =>
    Call.ByName(`${PROFILER_SERVICE_NAME}.StartProfiling`, traceLimitSeconds),
  );
}

export function stopProfiling() {
  return withApiLogging("StopProfiling", undefined, () =>
    Call.ByName(`${PROFILER_SERVICE_NAME}.StopProfiling`),
  );
}

export function openProfilerDirectory() {
  return withApiLogging("OpenProfilerDirectory", undefined, () =>
    Call.ByName(`${PROFILER_SERVICE_NAME}.OpenProfilerDirectory`),
  );
}
