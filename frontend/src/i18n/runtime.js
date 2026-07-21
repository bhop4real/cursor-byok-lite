import { computed, ref } from "vue";
import { Call, Events, Window } from "@wailsio/runtime";
import {
  DEFAULT_LOCALE,
  LOCALE_OPTIONS,
  LOCALE_STORAGE_KEY,
  LOCALE_STORAGE_SOURCE_KEY,
  SOURCE_LOCALE,
  SUPPORTED_LOCALES,
} from "@/i18n/config";
import zhCNMessages from "@/i18n/locales/zh-CN.json";
import enUSMessages from "@/i18n/locales/en-US.json";
import jaJPMessages from "@/i18n/locales/ja-JP.json";

const WINDOW_SERVICE_NAME = "cursor/internal/bridge.WindowService";
const WINDOW_TITLE_MESSAGES = {
  main: ["a080764958512426", "Cursor 助手"],
  config: ["df3d58c7d84b85f2", "设置"],
  modelConfig: ["8cbcf741e727dbf7", "模型配置"],
  modelEditorAdd: ["1bc77f5ab979f4c1", "新增模型配置"],
  modelEditorEdit: ["d2243e1d44b2a94e", "编辑模型配置"],
};
const localeMessages = {
  "zh-CN": zhCNMessages,
  "en-US": enUSMessages,
  "ja-JP": jaJPMessages,
};

const languageLocaleMap = {
  zh: "zh-CN",
  en: "en-US",
  ja: "ja-JP",
};

function isSupportedLocale(locale) {
  return SUPPORTED_LOCALES.includes(locale);
}

function matchSupportedLocale(locale) {
  const normalized = String(locale || "").trim().replace(/_/g, "-");
  if (!normalized) {
    return "";
  }

  const lowered = normalized.toLowerCase();
  const exactMatch = SUPPORTED_LOCALES.find((supportedLocale) => supportedLocale.toLowerCase() === lowered);
  if (exactMatch) {
    return exactMatch;
  }

  const primaryLanguage = lowered.split("-")[0];
  return languageLocaleMap[primaryLanguage] || "";
}

function getSystemLocaleCandidates() {
  const candidates = [];

  if (typeof navigator !== "undefined") {
    if (Array.isArray(navigator.languages)) {
      candidates.push(...navigator.languages);
    }
    candidates.push(navigator.language);
  }

  if (typeof Intl !== "undefined" && typeof Intl.DateTimeFormat === "function") {
    candidates.push(Intl.DateTimeFormat().resolvedOptions()?.locale);
  }

  return candidates;
}

function resolveSystemLocale() {
  for (const candidate of getSystemLocaleCandidates()) {
    const matchedLocale = matchSupportedLocale(candidate);
    if (matchedLocale) {
      return matchedLocale;
    }
  }

  return DEFAULT_LOCALE;
}

function resolveInitialLocale() {
  if (typeof window === "undefined" || typeof window.localStorage === "undefined") {
    return resolveSystemLocale();
  }

  const storedLocale = window.localStorage.getItem(LOCALE_STORAGE_KEY);
  const storedSource = window.localStorage.getItem(LOCALE_STORAGE_SOURCE_KEY);
  if (storedSource === "manual") {
    return matchSupportedLocale(storedLocale) || resolveSystemLocale();
  }

  window.localStorage.removeItem(LOCALE_STORAGE_KEY);
  window.localStorage.removeItem(LOCALE_STORAGE_SOURCE_KEY);
  return resolveSystemLocale();
}

function applyLocaleToDocument(locale) {
  if (typeof document !== "undefined") {
    document.documentElement.lang = locale;
  }
}

function persistManualLocale(locale) {
  if (typeof window === "undefined" || typeof window.localStorage === "undefined") {
    return;
  }

  window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  window.localStorage.setItem(LOCALE_STORAGE_SOURCE_KEY, "manual");
}

function windowTitles(locale) {
  const activeMessages = localeMessages[locale] || {};
  const sourceMessages = localeMessages[SOURCE_LOCALE] || {};
  return Object.fromEntries(
    Object.entries(WINDOW_TITLE_MESSAGES).map(([key, [id, fallback]]) => [
      key,
      activeMessages[id] || sourceMessages[id] || fallback,
    ]),
  );
}

function isMainWindow() {
  if (typeof window === "undefined") {
    return false;
  }
  const routePath = String(window.location.hash || "#/").split("?")[0];
  return routePath === "#/" || routePath === "" || routePath === "#";
}

async function syncNativeWindowTitles(locale) {
  const titles = windowTitles(locale);
  const updates = [Call.ByName(`${WINDOW_SERVICE_NAME}.UpdateWindowTitles`, titles)];
  if (isMainWindow()) {
    const windowID = await Window.ID();
    updates.push(Call.ByName(`${WINDOW_SERVICE_NAME}.SetWindowTitle`, windowID, titles.main));
  }
  await Promise.all(updates);
}

function resolveMessage(id, fallback) {
  const activeMessages = localeMessages[currentLocale.value] || {};
  const sourceMessages = localeMessages[SOURCE_LOCALE] || {};
  return activeMessages[id] || sourceMessages[id] || fallback || "";
}

function interpolateMessage(template, args = []) {
  return template.replace(/\{(\d+)\}/g, (_match, index) => {
    const value = args[Number(index)];
    return value == null ? "" : String(value);
  });
}

class LocalizedText extends String {
  constructor(id, fallback, args = null) {
    super(fallback);
    this.id = id;
    this.fallback = fallback;
    this.args = args;
  }

  toString() {
    const text = resolveMessage(this.id, this.fallback);
    return Array.isArray(this.args) ? interpolateMessage(text, this.args) : text;
  }

  valueOf() {
    return this.toString();
  }

  toJSON() {
    return this.toString();
  }

  [Symbol.toPrimitive]() {
    return this.toString();
  }
}

const currentLocale = ref(resolveInitialLocale());
applyLocaleToDocument(currentLocale.value);
Events.Emit("locale:changed", currentLocale.value);
Events.On("locale:changed", (event) => {
  const nextLocale = matchSupportedLocale(event?.data ?? event);
  if (!nextLocale || nextLocale === currentLocale.value) {
    return;
  }
  currentLocale.value = nextLocale;
  applyLocaleToDocument(nextLocale);
  void syncNativeWindowTitles(nextLocale).catch(() => {});
});

const localizedCache = new Map();

export function getLocale() {
  return currentLocale.value;
}

export function setLocale(locale) {
  const nextLocale = matchSupportedLocale(locale) || DEFAULT_LOCALE;
  currentLocale.value = nextLocale;
  persistManualLocale(nextLocale);
  applyLocaleToDocument(nextLocale);
  Events.Emit("locale:changed", nextLocale);
  void syncNativeWindowTitles(nextLocale).catch(() => {});
  return nextLocale;
}

export function useLocale() {
  return {
    locale: currentLocale,
    localeOptions: LOCALE_OPTIONS,
    currentLocale: computed(() => currentLocale.value),
    setLocale,
  };
}

export function localized(id, fallback) {
  const cacheKey = `${id}:${fallback}`;
  if (!localizedCache.has(cacheKey)) {
    localizedCache.set(cacheKey, new LocalizedText(id, fallback));
  }
  return localizedCache.get(cacheKey);
}

export function localizedTemplate(id, fallback, args = []) {
  return new LocalizedText(id, fallback, args);
}

export function installI18nRuntime(app) {
  app.config.globalProperties.$ls = localized;
  app.config.globalProperties.$lt = localizedTemplate;
  void syncNativeWindowTitles(currentLocale.value).catch(() => {});
}
