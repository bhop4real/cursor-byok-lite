<script setup>
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import LocaleSelect from "@/components/LocaleSelect.vue";
import Select from "@/components/ui/Select.vue";
import Switch from "@/components/ui/Switch.vue";
import { showModal } from "@/composables/useModal";
import {
  getEditableConfigMetadata,
  getProfilerStatus,
  loadUserConfig,
  openProfilerDirectory,
  openSettingsDirectory,
  patchUserConfig,
  startProfiling,
  stopProfiling,
} from "@/services/clientApi";
import {
  appState,
  openModelConfigWindow,
  reloadUserConfig,
  toUserError,
} from "@/state/appState";
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";

const routeModeLabels = {
  local: "本地服务模式",
  upstream: "直连 Cursor 模式",
};
const responseLanguageLabels = {
  auto: "自动跟随当前请求",
  "en-US": "English",
  "zh-CN": "简体中文",
  "ja-JP": "日本語",
};
const traceDurationOptions = [
  { label: "1 分钟", value: "60" },
  { label: "5 分钟", value: "300" },
  { label: "10 分钟", value: "600" },
  { label: "15 分钟", value: "900" },
];

const draft = reactive({
  log: false,
  disableUpdates: false,
  compactContextTools: false,
  responseLanguage: "auto",
  providerStreamIdleTimeout: 240,
  backendListenAddr: "127.0.0.1:18090",
  proxyListenAddr: "127.0.0.1:18080",
  routingMode: "local",
  includeCacheWriteInHitRate: false,
  refreshIntervalSeconds: 60,
});
const loading = ref(true);
const saving = ref(false);
const persistedConfig = ref(null);
const editableFields = ref({});
const profilerBusy = ref(false);
const profilerStatus = reactive({
  state: "idle",
  sessionId: "",
  directory: "",
  startedAt: "",
  stoppedAt: "",
  traceLimitSeconds: 300,
  autoStopped: false,
  error: "",
});
const traceDurationSeconds = ref("300");
let profilerRefreshTimer = null;

const routeModeOptions = computed(() => enumOptions("routing.mode", routeModeLabels, ["local", "upstream"]));
const responseLanguageOptions = computed(() => enumOptions(
  "responseLanguage",
  responseLanguageLabels,
  ["auto", "en-US", "zh-CN", "ja-JP"],
));
const providerTimeoutMinimum = computed(() => fieldMinimum("providerStreamIdleTimeout", 30));
const refreshIntervalMinimum = computed(() => fieldMinimum("homeMetrics.refreshIntervalSeconds", 1));
const profilerRunning = computed(() => profilerStatus.state === "running");
const profilerStatusText = computed(() => {
  if (profilerStatus.state === "running") {
    return "正在采集";
  }
  if (profilerStatus.state === "stopped") {
    return profilerStatus.autoStopped ? "已到时自动停止" : "已停止";
  }
  return "未开始";
});
const profilerStatusClass = computed(() => {
  if (profilerStatus.state === "running") {
    return "text-[#10AD5D]";
  }
  if (profilerStatus.error) {
    return "text-[#fca5a5]";
  }
  return "text-[#a3a3a3]";
});

function applyMetadata(metadata) {
  const fields = Array.isArray(metadata?.fields) ? metadata.fields : [];
  editableFields.value = Object.fromEntries(
    fields
      .filter((field) => field && typeof field.path === "string")
      .map((field) => [field.path, field]),
  );
}

function fieldMetadata(path) {
  return editableFields.value[path] || {};
}

function fieldDefault(path, fallback) {
  const value = fieldMetadata(path).default;
  return value === undefined || value === null ? fallback : value;
}

function fieldMinimum(path, fallback) {
  const value = Number(fieldMetadata(path).minimum);
  return Number.isFinite(value) ? value : fallback;
}

function enumOptions(path, labels, fallbackValues) {
  const values = Array.isArray(fieldMetadata(path).enum) && fieldMetadata(path).enum.length
    ? fieldMetadata(path).enum
    : fallbackValues;
  return values.map((value) => ({ label: labels[value] || value, value }));
}

function normalizeEnum(path, value, fallback) {
  const allowed = enumOptions(path, {}, [fallback]).map((option) => option.value);
  return allowed.includes(value) ? value : fallback;
}

function applyConfig(config) {
  const source = config && typeof config === "object" ? config : {};
  persistedConfig.value = source;
  draft.log = Boolean(source.log ?? fieldDefault("log", false));
  draft.disableUpdates = Boolean(source.disableUpdates ?? fieldDefault("disableUpdates", false));
  draft.compactContextTools = Boolean(
    source.compactContextTools ?? fieldDefault("compactContextTools", false),
  );
  draft.responseLanguage = normalizeEnum(
    "responseLanguage",
    source.responseLanguage,
    String(fieldDefault("responseLanguage", "auto")),
  );
  draft.providerStreamIdleTimeout = Math.max(
    providerTimeoutMinimum.value,
    Number(source.providerStreamIdleTimeout) || Number(fieldDefault("providerStreamIdleTimeout", 240)),
  );
  draft.backendListenAddr = String(
    source.backendListenAddr || fieldDefault("backendListenAddr", "127.0.0.1:18090"),
  );
  draft.proxyListenAddr = String(
    source.proxyListenAddr || fieldDefault("proxyListenAddr", "127.0.0.1:18080"),
  );
  draft.routingMode = normalizeEnum(
    "routing.mode",
    source.routing?.mode,
    String(fieldDefault("routing.mode", "local")),
  );
  draft.includeCacheWriteInHitRate = Boolean(
    source.homeMetrics?.includeCacheWriteInHitRate
      ?? fieldDefault("homeMetrics.includeCacheWriteInHitRate", false),
  );
  draft.refreshIntervalSeconds = Math.max(
    refreshIntervalMinimum.value,
    Number(source.homeMetrics?.refreshIntervalSeconds)
      || Number(fieldDefault("homeMetrics.refreshIntervalSeconds", 60)),
  );
}

function applyProfilerStatus(status) {
  const source = status && typeof status === "object" ? status : {};
  profilerStatus.state = String(source.state || "idle");
  profilerStatus.sessionId = String(source.sessionId || "");
  profilerStatus.directory = String(source.directory || "");
  profilerStatus.startedAt = String(source.startedAt || "");
  profilerStatus.stoppedAt = String(source.stoppedAt || "");
  profilerStatus.traceLimitSeconds = Number(source.traceLimitSeconds) || 300;
  profilerStatus.autoStopped = Boolean(source.autoStopped);
  profilerStatus.error = String(source.error || "");
  if (profilerStatus.state !== "running" && profilerRefreshTimer) {
    window.clearInterval(profilerRefreshTimer);
    profilerRefreshTimer = null;
  }
}

function parseInteger(value, minimum, label) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < minimum) {
    throw new Error(`${label}必须是不小于 ${minimum} 的整数`);
  }
  return parsed;
}

function normalizeListenAddress(value, label) {
  const address = String(value || "").trim();
  const bracketed = address.match(/^\[([^\]]+)]:(\d+)$/);
  const plain = address.match(/^([^:\s]+):(\d+)$/);
  const match = bracketed || plain;
  if (!match) {
    throw new Error(`${label}必须使用 host:port 格式`);
  }
  const port = Number(match[2]);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`${label}端口必须在 1 到 65535 之间`);
  }
  return address;
}

function buildConfigPatch() {
  return {
    log: Boolean(draft.log),
    disableUpdates: Boolean(draft.disableUpdates),
    compactContextTools: Boolean(draft.compactContextTools),
    responseLanguage: draft.responseLanguage,
    providerStreamIdleTimeout: parseInteger(
      draft.providerStreamIdleTimeout,
      providerTimeoutMinimum.value,
      "Provider 流空闲超时",
    ),
    backendListenAddr: normalizeListenAddress(draft.backendListenAddr, "Backend 监听地址"),
    proxyListenAddr: normalizeListenAddress(draft.proxyListenAddr, "代理监听地址"),
    routing: { mode: draft.routingMode },
    homeMetrics: {
      includeCacheWriteInHitRate: Boolean(draft.includeCacheWriteInHitRate),
      refreshIntervalSeconds: parseInteger(
        draft.refreshIntervalSeconds,
        refreshIntervalMinimum.value,
        "首页指标刷新间隔",
      ),
    },
  };
}

async function showActionError(title, error) {
  await showModal({
    title,
    content: String(error || "服务错误").trim() || "服务错误",
    showCancel: false,
  });
}

async function refreshProfilerStatus() {
  applyProfilerStatus(await getProfilerStatus());
}

function ensureProfilerRefreshTimer() {
  if (profilerRefreshTimer) {
    return;
  }
  profilerRefreshTimer = window.setInterval(() => {
    void refreshProfilerStatus().catch(() => {});
  }, 1000);
}

async function handleSaveConfig() {
  if (!persistedConfig.value || saving.value) {
    return;
  }
  saving.value = true;
  try {
    const previous = persistedConfig.value;
    const patch = buildConfigPatch();
    const listenersChanged = previous.backendListenAddr !== patch.backendListenAddr
      || previous.proxyListenAddr !== patch.proxyListenAddr;
    const next = await patchUserConfig(patch);
    applyConfig(next);
    await reloadUserConfig().catch(() => {});
    await showModal({
      title: "提示",
      content: listenersChanged
        ? "配置已保存。监听地址已更新；运行中的服务已自动重启，其他请求设置从下一次请求开始生效。"
        : "配置已保存。请求相关设置会从下一次模型请求开始生效。",
      showCancel: false,
    });
  } catch (error) {
    await showActionError("保存失败", toUserError(error));
  } finally {
    saving.value = false;
  }
}

async function handleStartProfiling() {
  if (profilerBusy.value || profilerRunning.value) {
    return;
  }
  profilerBusy.value = true;
  try {
    applyProfilerStatus(await startProfiling(Number(traceDurationSeconds.value)));
    ensureProfilerRefreshTimer();
  } catch (error) {
    await showActionError("启动性能分析失败", toUserError(error));
  } finally {
    profilerBusy.value = false;
  }
}

async function handleStopProfiling() {
  if (profilerBusy.value || !profilerRunning.value) {
    return;
  }
  profilerBusy.value = true;
  try {
    applyProfilerStatus(await stopProfiling());
  } catch (error) {
    await showActionError("停止性能分析失败", toUserError(error));
  } finally {
    profilerBusy.value = false;
  }
}

async function handleOpenProfilerDirectory() {
  try {
    await openProfilerDirectory();
  } catch (error) {
    await showActionError("打开失败", toUserError(error));
  }
}

async function handleOpenSettingsDirectory() {
  try {
    await openSettingsDirectory();
  } catch (error) {
    await showActionError("打开失败", toUserError(error));
  }
}

async function handleOpenModelConfig() {
  try {
    await openModelConfigWindow();
  } catch (error) {
    await showActionError("打开失败", toUserError(error));
  }
}

onMounted(async () => {
  try {
    const [metadata, config, status] = await Promise.all([
      getEditableConfigMetadata(),
      loadUserConfig(),
      getProfilerStatus(),
    ]);
    applyMetadata(metadata);
    applyConfig(config);
    applyProfilerStatus(status);
    if (profilerStatus.state === "running") {
      ensureProfilerRefreshTimer();
    }
  } catch (error) {
    await showActionError("加载设置失败", toUserError(error));
  } finally {
    loading.value = false;
  }
});

onBeforeUnmount(() => {
  if (profilerRefreshTimer) {
    window.clearInterval(profilerRefreshTimer);
    profilerRefreshTimer = null;
  }
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3 overflow-y-auto px-4 pb-4 text-[#e5e5e5]">
    <Card>
      <div class="flex items-center justify-between gap-4">
        <div>
          <h2 class="text-base font-medium text-white">本地设置</h2>
          <div class="text-sm text-[#a3a3a3]">
            配置保存到 <code>~/.cursor-local-assistant-v2/config.yaml</code>；未知字段和模型密钥不会被此页面改写。
          </div>
        </div>
        <div class="center-row gap-2">
          <Button variant="default" @click="handleOpenSettingsDirectory">打开设置目录</Button>
          <Button variant="primary" :disabled="loading || saving" @click="handleSaveConfig">
            {{ saving ? "保存中..." : "保存设置" }}
          </Button>
        </div>
      </div>
    </Card>

    <Card>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="flex flex-col gap-2">
          <div>
            <h2 class="text-sm font-medium text-white">运行模式</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">控制请求使用本地模型渠道或原始 Cursor 上游；下次请求生效。</div>
          </div>
          <Select v-model="draft.routingMode" :options="routeModeOptions" :disabled="loading" placeholder="选择模式" />
        </div>
        <div class="flex flex-col gap-2">
          <div>
            <h2 class="text-sm font-medium text-white">Provider 流空闲超时</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">
              超过该时间没有流事件时终止请求，最小 {{ providerTimeoutMinimum }} 秒；下次请求生效。
            </div>
          </div>
          <input
            v-model="draft.providerStreamIdleTimeout"
            type="number"
            :min="providerTimeoutMinimum"
            step="1"
            :disabled="loading"
            class="h-9 rounded-[6px] border border-[#3f3f3f] bg-[#232323] px-3 text-sm text-[#e5e5e5] outline-none focus:border-[#10AD5D] disabled:opacity-60"
          />
        </div>
      </div>
      <div class="mt-4 grid grid-cols-1 gap-4 border-t border-[#343434] pt-4 md:grid-cols-2">
        <Switch
          :enabled="draft.disableUpdates"
          :disabled="loading"
          label="禁用自动更新"
          description="保存后下次启动应用生效。"
          enabled-text="已禁用更新"
          disabled-text="更新已启用"
          @change="draft.disableUpdates = $event"
        />
        <Switch
          :enabled="draft.log"
          :disabled="loading"
          label="诊断日志"
          description="立即记录后端运行事件，不影响离线性能分析。"
          enabled-text="日志已启用"
          disabled-text="日志已关闭"
          @change="draft.log = $event"
        />
        <Switch
          :enabled="draft.compactContextTools"
          :disabled="loading"
          label="紧凑上下文与工具实验"
          description="仅为启用后新建的会话选择紧凑 Provider 投影；已有会话不会迁移或改写。"
          enabled-text="新会话将使用实验投影"
          disabled-text="新会话使用基线投影"
          @change="draft.compactContextTools = $event"
        />
      </div>
    </Card>

    <Card>
      <div class="mb-4">
        <h2 class="text-sm font-medium text-white">服务监听地址</h2>
        <div class="text-xs leading-5 text-[#a3a3a3]">
          使用 host:port 格式。服务运行时保存会执行受控重启；新地址启动失败时会回滚配置并尝试恢复原服务。
        </div>
      </div>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <label class="flex flex-col gap-2">
          <span class="text-xs font-medium text-[#d4d4d4]">Backend 监听地址</span>
          <input
            v-model="draft.backendListenAddr"
            type="text"
            spellcheck="false"
            :disabled="loading"
            class="h-9 rounded-[6px] border border-[#3f3f3f] bg-[#232323] px-3 font-mono text-sm text-[#e5e5e5] outline-none focus:border-[#10AD5D] disabled:opacity-60"
          />
        </label>
        <label class="flex flex-col gap-2">
          <span class="text-xs font-medium text-[#d4d4d4]">代理监听地址</span>
          <input
            v-model="draft.proxyListenAddr"
            type="text"
            spellcheck="false"
            :disabled="loading"
            class="h-9 rounded-[6px] border border-[#3f3f3f] bg-[#232323] px-3 font-mono text-sm text-[#e5e5e5] outline-none focus:border-[#10AD5D] disabled:opacity-60"
          />
        </label>
      </div>
    </Card>

    <Card>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="flex flex-col gap-2">
          <div>
            <h2 class="text-sm font-medium text-white">界面语言</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">只影响配置界面，切换后立即生效。</div>
          </div>
          <LocaleSelect wrapper-class="w-full" />
        </div>
        <div class="flex flex-col gap-2">
          <div>
            <h2 class="text-sm font-medium text-white">助手响应语言</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">独立于界面语言；自动模式只检测当前用户请求。</div>
          </div>
          <Select
            v-model="draft.responseLanguage"
            :options="responseLanguageOptions"
            :disabled="loading"
            placeholder="选择响应语言"
          />
        </div>
      </div>
    </Card>

    <Card>
      <div class="mb-4">
        <h2 class="text-sm font-medium text-white">首页指标</h2>
        <div class="text-xs leading-5 text-[#a3a3a3]">控制本地首页统计口径和刷新频率，保存后立即生效。</div>
      </div>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Switch
          :enabled="draft.includeCacheWriteInHitRate"
          :disabled="loading"
          label="命中率包含缓存写入"
          description="将 cache write token 计入缓存命中率分母和命中量。"
          enabled-text="包含缓存写入"
          disabled-text="只统计缓存读取"
          @change="draft.includeCacheWriteInHitRate = $event"
        />
        <label class="flex flex-col gap-2">
          <span class="text-sm font-medium text-white">指标刷新间隔</span>
          <span class="text-xs text-[#a3a3a3]">最小 {{ refreshIntervalMinimum }} 秒。</span>
          <input
            v-model="draft.refreshIntervalSeconds"
            type="number"
            :min="refreshIntervalMinimum"
            step="1"
            :disabled="loading"
            class="h-9 rounded-[6px] border border-[#3f3f3f] bg-[#232323] px-3 text-sm text-[#e5e5e5] outline-none focus:border-[#10AD5D] disabled:opacity-60"
          />
        </label>
      </div>
    </Card>

    <Card>
      <div class="flex flex-col gap-4">
        <div class="flex items-start justify-between gap-4">
          <div>
            <h2 class="text-sm font-medium text-white">离线性能分析</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">
              采集 CPU、goroutine、heap、block、mutex 和 runtime trace；不会写入请求正文、密钥或模型输出。
            </div>
          </div>
          <div class="shrink-0 text-sm" :class="profilerStatusClass">{{ profilerStatusText }}</div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <div class="w-[150px]">
            <Select
              v-model="traceDurationSeconds"
              :options="traceDurationOptions"
              :disabled="profilerRunning || profilerBusy"
              placeholder="采集时长"
            />
          </div>
          <Button
            v-if="!profilerRunning"
            variant="primary"
            :disabled="profilerBusy"
            @click="handleStartProfiling"
          >
            {{ profilerBusy ? "启动中..." : "开始采集" }}
          </Button>
          <Button
            v-else
            variant="primary"
            :disabled="profilerBusy"
            @click="handleStopProfiling"
          >
            {{ profilerBusy ? "停止中..." : "停止并保存" }}
          </Button>
          <Button variant="default" :disabled="profilerBusy" @click="handleOpenProfilerDirectory">
            打开分析目录
          </Button>
        </div>
        <div
          v-if="profilerStatus.directory"
          class="rounded-[8px] border border-[#343434] bg-[#202020] px-3 py-2"
        >
          <div class="text-[11px] uppercase tracking-[0.08em] text-[#666]">分析文件目录</div>
          <code class="mt-1 block break-all text-xs leading-5 text-[#d4d4d4]">{{ profilerStatus.directory }}</code>
        </div>
        <div v-if="profilerStatus.error" class="rounded-[8px] border border-[#4b1d1d] bg-[#2a1313] px-3 py-2 text-xs text-[#fca5a5]">
          {{ profilerStatus.error }}
        </div>
      </div>
    </Card>

    <Card>
      <div class="flex items-center justify-between gap-4">
        <div>
          <h2 class="text-sm font-medium text-white">模型渠道</h2>
          <div class="text-xs leading-5 text-[#a3a3a3]">已配置 {{ appState.modelAdapters.length }} 个模型适配器。</div>
        </div>
        <Button variant="primary" @click="handleOpenModelConfig">打开模型配置</Button>
      </div>
    </Card>
  </div>
</template>
