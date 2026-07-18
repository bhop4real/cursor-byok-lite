<script setup>
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import LocaleSelect from "@/components/LocaleSelect.vue";
import Select from "@/components/ui/Select.vue";
import Switch from "@/components/ui/Switch.vue";
import { showModal } from "@/composables/useModal";
import {
  getProfilerStatus,
  loadUserConfig,
  openProfilerDirectory,
  openSettingsDirectory,
  saveUserConfig,
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

const routeModeOptions = [
  { label: "本地服务模式", value: "local" },
  { label: "直连 Cursor 模式", value: "upstream" },
];
const responseLanguageOptions = [
  { label: "自动跟随当前请求", value: "auto" },
  { label: "English", value: "en-US" },
  { label: "简体中文", value: "zh-CN" },
  { label: "日本語", value: "ja-JP" },
];
const traceDurationOptions = [
  { label: "1 分钟", value: "60" },
  { label: "5 分钟", value: "300" },
  { label: "10 分钟", value: "600" },
  { label: "15 分钟", value: "900" },
];

const draft = reactive({
  log: false,
  disableUpdates: false,
  responseLanguage: "auto",
  providerStreamIdleTimeout: 240,
  routingMode: "local",
});
const loading = ref(true);
const saving = ref(false);
const persistedConfig = ref(null);
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

function normalizeResponseLanguage(value) {
  return ["auto", "en-US", "zh-CN", "ja-JP"].includes(value) ? value : "auto";
}

function applyConfig(config) {
  const source = config && typeof config === "object" ? config : {};
  persistedConfig.value = source;
  draft.log = Boolean(source.log);
  draft.disableUpdates = Boolean(source.disableUpdates);
  draft.responseLanguage = normalizeResponseLanguage(source.responseLanguage);
  draft.providerStreamIdleTimeout = Math.max(30, Number(source.providerStreamIdleTimeout) || 240);
  draft.routingMode = source.routing?.mode === "upstream" ? "upstream" : "local";
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
    const timeout = Number(draft.providerStreamIdleTimeout);
    if (!Number.isInteger(timeout) || timeout < 30) {
      await showActionError("保存失败", "Provider 流空闲超时必须是不小于 30 的整数秒数");
      return;
    }
    const current = persistedConfig.value;
    const payload = {
      ...current,
      log: draft.log,
      disableUpdates: draft.disableUpdates,
      responseLanguage: draft.responseLanguage,
      providerStreamIdleTimeout: timeout,
      routing: {
        ...(current.routing || {}),
        mode: draft.routingMode,
      },
    };
    await saveUserConfig(payload);
    const next = await loadUserConfig();
    applyConfig(next);
    await reloadUserConfig().catch(() => {});
    await showModal({
      title: "提示",
      content: "本地配置已保存，响应语言会从下一次模型请求开始生效。",
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
    const [config, status] = await Promise.all([loadUserConfig(), getProfilerStatus()]);
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
            配置保存到 <code>~/.cursor-local-assistant-v2/config.yaml</code>
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
            <div class="text-xs leading-5 text-[#a3a3a3]">控制请求使用本地模型渠道或原始 Cursor 上游。</div>
          </div>
          <Select v-model="draft.routingMode" :options="routeModeOptions" :disabled="loading" placeholder="选择模式" />
        </div>
        <div class="flex flex-col gap-2">
          <div>
            <h2 class="text-sm font-medium text-white">Provider 流空闲超时</h2>
            <div class="text-xs leading-5 text-[#a3a3a3]">超过该时间没有有效内容时终止请求，最小 30 秒。</div>
          </div>
          <input
            v-model="draft.providerStreamIdleTimeout"
            type="number"
            min="30"
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
          description="保存后下次启动生效。"
          enabled-text="已禁用更新"
          disabled-text="更新已启用"
          @change="draft.disableUpdates = $event"
        />
        <Switch
          :enabled="draft.log"
          :disabled="loading"
          label="诊断日志"
          description="记录后端运行事件，不影响离线性能分析。"
          enabled-text="日志已启用"
          disabled-text="日志已关闭"
          @change="draft.log = $event"
        />
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
