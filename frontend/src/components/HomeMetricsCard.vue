<script setup>
import CacheHitRateChart from "@/components/charts/CacheHitRateChart.vue";
import HomeMetricsTrendChart from "@/components/charts/HomeMetricsTrendChart.vue";
import Switch from "@/components/ui/Switch.vue";
import Tooltip from "@/components/ui/Tooltip.vue";
import { appState, saveIncludeCacheWriteInHitRate } from "@/state/appState";
import { formatCompactInteger, formatCompactUSD, formatInteger } from "@/utils/numberFormat";
import { computed, ref } from "vue";

const emit = defineEmits(["refresh"]);

const COST_PROFILES = [
  {
    id: "claude-opus-4.7",
    label: "Claude Opus 4.7",
    prices: {
      standard: { input: 5, cacheRead: 0.5, cacheWrite: 6.25, output: 25 },
    },
  },
  {
    id: "claude-opus-4.8",
    label: "Claude Opus 4.8",
    prices: {
      standard: { input: 5, cacheRead: 0.5, cacheWrite: 6.25, output: 25 },
    },
  },
  {
    id: "claude-fable-5",
    label: "Claude Fable 5",
    note: "缓存写入按 5 分钟价格估算；当前统计没有缓存 TTL 维度。",
    prices: {
      standard: { input: 10, cacheRead: 1, cacheWrite: 12.5, output: 50 },
    },
  },
  {
    id: "gpt-5.6-sol",
    label: "GPT 5.6 Sol",
    prices: {
      short: { input: 5, cacheRead: 0.5, cacheWrite: 6.25, output: 30 },
      long: { input: 10, cacheRead: 1, cacheWrite: 12.5, output: 45 },
    },
  },
  {
    id: "gpt-5.6-terra",
    label: "GPT 5.6 Terra",
    prices: {
      short: { input: 2.5, cacheRead: 0.25, cacheWrite: 3.125, output: 15 },
      long: { input: 5, cacheRead: 0.5, cacheWrite: 6.25, output: 22.5 },
    },
  },
  {
    id: "gpt-5.6-luna",
    label: "GPT 5.6 Luna",
    prices: {
      short: { input: 1, cacheRead: 0.1, cacheWrite: 1.25, output: 6 },
      long: { input: 2, cacheRead: 0.2, cacheWrite: 2.5, output: 9 },
    },
  },
  {
    id: "mimo-v2.5",
    label: "MiMo V2.5",
    prices: {
      standard: { input: 0.14, cacheRead: 0.0028, cacheWrite: 0.14, output: 0.28 },
    },
  },
  {
    id: "mimo-v2.5-pro",
    label: "MiMo V2.5 Pro",
    prices: {
      standard: { input: 0.435, cacheRead: 0.0036, cacheWrite: 0.435, output: 0.87 },
    },
  },
  {
    id: "deepseek-v4-flash-preview",
    label: "DeepSeek V4 Flash (Preview)",
    note: "预览版本；缓存写入按未命中缓存输入价格估算。",
    prices: {
      standard: { input: 0.14, cacheRead: 0.0028, cacheWrite: 0.14, output: 0.28 },
    },
  },
  {
    id: "deepseek-v4-pro-preview",
    label: "DeepSeek V4 Pro (Preview)",
    note: "预览版本；缓存写入按未命中缓存输入价格估算。",
    prices: {
      standard: { input: 0.435, cacheRead: 0.003625, cacheWrite: 0.435, output: 0.87 },
    },
  },
  {
    id: "kimi-k3",
    label: "Kimi K3",
    prices: {
      standard: { input: 3, cacheRead: 0.3, cacheWrite: 3, output: 15 },
    },
  },
];

const selectedCostProfileID = ref("claude-opus-4.8");
const selectedCostContextTier = ref("short");

const selectedCostProfile = computed(() =>
  COST_PROFILES.find((profile) => profile.id === selectedCostProfileID.value) || COST_PROFILES[0],
);

const selectedCostPrices = computed(() => {
  const prices = selectedCostProfile.value.prices;
  return prices[selectedCostContextTier.value] || prices.standard || prices.short;
});

const hasCostContextTiers = computed(() =>
  Object.prototype.hasOwnProperty.call(selectedCostProfile.value.prices, "short"),
);

const costContextTierLabel = computed(() =>
  selectedCostContextTier.value === "long" ? "长上下文" : "短上下文",
);

function selectCostProfile(profileID) {
  selectedCostProfileID.value = profileID;
  if (!Object.prototype.hasOwnProperty.call(selectedCostProfile.value.prices, selectedCostContextTier.value)) {
    selectedCostContextTier.value = hasCostContextTiers.value ? "short" : "standard";
  }
}


const props = defineProps({
  metrics: {
    type: Object,
    required: true,
  },
  loading: {
    type: Boolean,
    default: false,
  },
  error: {
    type: String,
    default: "",
  },
});

const homeMetricsConfigSaving = ref(false);
const homeMetricsConfigError = ref("");

function normalizeNumber(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return 0;
  }
  return Math.round(number);
}

function formatMetricValue(value) {
  const full = formatInteger(value);
  const compact = formatCompactInteger(value);
  return full === compact ? full : `${full} (${compact})`;
}

function formatRateLabel(value) {
  const rate = Number(value);
  if (!Number.isFinite(rate)) {
    return "暂无数据";
  }
  return `${(Math.max(0, Math.min(1, rate)) * 100).toFixed(2)}%`;
}

function calculateRate(numerator, denominator) {
  const top = normalizeNumber(numerator);
  const bottom = normalizeNumber(denominator);
  if (bottom <= 0) {
    return null;
  }
  return top / bottom;
}

function priceTokens(tokens, pricePerMillion) {
  return (normalizeNumber(tokens) / 1_000_000) * pricePerMillion;
}

function formatUSD(value) {
  const amount = Number(value);
  if (!Number.isFinite(amount)) {
    return "$0.00";
  }
  if (amount > 0 && amount < 0.01) {
    return "<$0.01";
  }
  return `$${amount.toFixed(2)}`;
}

const cacheReadTokensTotal = computed(() => normalizeNumber(props.metrics?.cacheReadTokens));
const cacheWriteTokensTotal = computed(() => normalizeNumber(props.metrics?.cacheWriteTokens));

const inputTokensTotal = computed(() => {
  const promptTokensTotal = normalizeNumber(props.metrics?.promptTokensTotal);
  return Math.max(0, promptTokensTotal - cacheReadTokensTotal.value - cacheWriteTokensTotal.value);
});

const defaultCacheHitRate = computed(() =>
  calculateRate(cacheReadTokensTotal.value, cacheReadTokensTotal.value + inputTokensTotal.value),
);

const cacheReuseRate = computed(() =>
  calculateRate(
    cacheReadTokensTotal.value,
    cacheReadTokensTotal.value + cacheWriteTokensTotal.value + inputTokensTotal.value,
  ),
);

const includeCacheWriteInHitRate = computed(() => appState.includeCacheWriteInHitRate);

const selectedCacheHitRate = computed(() =>
  includeCacheWriteInHitRate.value ? cacheReuseRate.value : defaultCacheHitRate.value,
);

const selectedCacheRateModeLabel = computed(() =>
  includeCacheWriteInHitRate.value ? "计入缓存创建" : "默认口径",
);

const validTurnsRate = computed(() => {
  const turnsTotal = normalizeNumber(props.metrics?.turnsTotal);
  if (turnsTotal <= 0) {
    return null;
  }
  return normalizeNumber(props.metrics?.validTurnsTotal) / turnsTotal;
});

const completionTokensTotal = computed(() => {
  const requestTokensTotal = normalizeNumber(props.metrics?.requestTokensTotal);
  const promptTokensTotal = normalizeNumber(props.metrics?.promptTokensTotal);
  return Math.max(0, requestTokensTotal - promptTokensTotal);
});

const estimatedTokenCost = computed(() => {
  const prices = selectedCostPrices.value;
  const input = priceTokens(inputTokensTotal.value, prices.input);
  const output = priceTokens(completionTokensTotal.value, prices.output);
  const cacheRead = priceTokens(cacheReadTokensTotal.value, prices.cacheRead);
  const cacheWrite = priceTokens(cacheWriteTokensTotal.value, prices.cacheWrite);
  return {
    input,
    output,
    cacheRead,
    cacheWrite,
    total: input + output + cacheRead + cacheWrite,
  };
});

const cacheTooltipContent = computed(() => {
  const formula = includeCacheWriteInHitRate.value
    ? "缓存读取 /（缓存读取 + 缓存创建 + 非缓存输入）"
    : "缓存读取 /（缓存读取 + 非缓存输入）";
  return [
    `当前：${formatRateLabel(selectedCacheHitRate.value)}`,
    `公式：${formula}`,
    `默认 ${formatRateLabel(defaultCacheHitRate.value)} / 计入创建 ${formatRateLabel(cacheReuseRate.value)}`,
  ].join("\n");
});

const turnsTooltipContent = computed(() =>
  [
    "按历史记录里扫描到的回合 summary 汇总。",
    "",
    `总轮次：${formatMetricValue(props.metrics?.turnsTotal)}`,
    `有效轮次：${formatMetricValue(props.metrics?.validTurnsTotal)}`,
    `异常轮次：${formatMetricValue(props.metrics?.invalidTurnsTotal)}`,
    `有效占比：${formatRateLabel(validTurnsRate.value)}`,
  ].join("\n"),
);

const tokensTooltipContent = computed(() =>
  [
    "总请求 Token 包含 Prompt 和模型输出。",
    "",
    `总请求：${formatMetricValue(props.metrics?.requestTokensTotal)}`,
    `Prompt：${formatMetricValue(props.metrics?.promptTokensTotal)}`,
    `输出推算：${formatMetricValue(completionTokensTotal.value)}`,
    `非缓存输入：${formatMetricValue(inputTokensTotal.value)}`,
    `缓存读取：${formatMetricValue(cacheReadTokensTotal.value)}`,
    `缓存写入：${formatMetricValue(cacheWriteTokensTotal.value)}`,
    "",
    "缓存读写已计入 Prompt 侧统计。",
  ].join("\n"),
);

const costTooltipContent = computed(() => {
  const prices = selectedCostPrices.value;
  const profileNote = selectedCostProfile.value.note ? `\n${selectedCostProfile.value.note}` : "";
  return [
    `按 ${selectedCostProfile.value.label}（${hasCostContextTiers.value ? costContextTierLabel.value : "标准价格"}）套算全部累计 Token。${profileNote}`,
    `缓存统计策略：${selectedCacheRateModeLabel.value}（${formatRateLabel(selectedCacheHitRate.value)}）`,
    "",
    `普通输入：${formatMetricValue(inputTokensTotal.value)} × $${prices.input}/1M = ${formatUSD(estimatedTokenCost.value.input)}`,
    `模型输出：${formatMetricValue(completionTokensTotal.value)} × $${prices.output}/1M = ${formatUSD(estimatedTokenCost.value.output)}`,
    `缓存读取：${formatMetricValue(cacheReadTokensTotal.value)} × $${prices.cacheRead}/1M = ${formatUSD(estimatedTokenCost.value.cacheRead)}`,
    `缓存写入：${formatMetricValue(cacheWriteTokensTotal.value)} × $${prices.cacheWrite}/1M = ${formatUSD(estimatedTokenCost.value.cacheWrite)}`,
    "",
    `合计：${formatUSD(estimatedTokenCost.value.total)}`,
  ].join("\n");
});

async function toggleIncludeCacheWriteInHitRate(value) {
  const nextValue = Boolean(value);
  homeMetricsConfigSaving.value = true;
  homeMetricsConfigError.value = "";
  try {
    const result = await saveIncludeCacheWriteInHitRate(nextValue);
    if (!result?.ok) {
      homeMetricsConfigError.value = result?.error || "保存失败";
    }
  } catch (error) {
    homeMetricsConfigError.value = error?.message || "保存失败";
  } finally {
    homeMetricsConfigSaving.value = false;
  }
}
</script>

<template>
  <div>
    <div class="flex flex-col gap-4">
      <div class="flex items-center justify-between gap-4 h-[32px]">
        <div class="flex flex-col gap-1 shrink-0">
          <h2 class="text-[14px] font-medium text-white/80">会话统计</h2>
        </div>
        <div class="flex-1 center-row justify-center gap-2">
          <span class="text-xs text-[#6f6f6f] shrink-0">估算模型</span>
          <select
            :value="selectedCostProfileID"
            class="h-[24px] rounded-[4px] border border-[#3b3b3b] bg-[#1f1f1f] px-1.5 text-[11px] text-[#cfcfcf] outline-none"
            aria-label="估算模型"
            @change="selectCostProfile($event.target.value)"
          >
            <option v-for="profile in COST_PROFILES" :key="profile.id" :value="profile.id">
              {{ profile.label }}
            </option>
          </select>
          <select
            v-if="hasCostContextTiers"
            v-model="selectedCostContextTier"
            class="h-[24px] rounded-[4px] border border-[#3b3b3b] bg-[#1f1f1f] px-1.5 text-[11px] text-[#cfcfcf] outline-none"
            aria-label="上下文价格"
          >
            <option value="short">短上下文</option>
            <option value="long">长上下文</option>
          </select>
        </div>
        <div
          class="center-row justify-end shrink-0 gap-2 text-xs text-[#6f6f6f]"
        >
          <span>{{ appState.homeMetricsRefreshIntervalSeconds }} 秒自动刷新</span>
          <button
            type="button"
            class="center-row justify-center h-[24px] w-[24px] rounded-[6px] border border-[#3b3b3b] bg-[#242424] text-[#9d9d9d] transition-colors duration-150 hover:border-[#4c4c4c] hover:text-white disabled:cursor-not-allowed disabled:opacity-60"
            :disabled="loading"
            :title="loading ? '刷新中' : '刷新统计'"
            @click="emit('refresh')"
          >
            <span
              class="icon-[mdi--refresh] text-[14px]"
              :class="{ '!animate-spin': loading }"
            ></span>
          </button>
        </div>
      </div>

      <div
        class="mt-[-4px] grid grid-cols-4 gap-0 overflow-hidden rounded-[8px] border border-[#343434] bg-[#242424] h-[152px]"
      >
        <div class="min-w-0 px-4 py-4 flex flex-col justify-between">
          <div class="center-row justify-start gap-1 text-xs text-[#7f7f7f]">
            <span>缓存命中率</span>
            <Tooltip>
              <div class="w-[280px] space-y-3">
                <div class="border-b border-[#343434] pb-3">
                  <Switch
                    compact
                    label="计入缓存创建"
                    description="开启后把缓存创建纳入分母"
                    enabled-text="当前按复用率口径显示"
                    disabled-text="当前按默认命中率口径显示"
                    :enabled="includeCacheWriteInHitRate"
                    :busy="homeMetricsConfigSaving"
                    :disabled="homeMetricsConfigSaving"
                    @change="toggleIncludeCacheWriteInHitRate"
                  />
                </div>
                <div class="whitespace-pre-wrap">{{ cacheTooltipContent }}</div>
                <div v-if="homeMetricsConfigError" class="text-[11px] text-[#f87171]">
                  {{ homeMetricsConfigError }}
                </div>
              </div>
            </Tooltip>
          </div>
          <CacheHitRateChart :rate="selectedCacheHitRate" />
        </div>

        <div
          class="min-w-0 border-l border-[#343434] px-4 py-4 flex flex-col justify-between"
        >
          <div class="center-row justify-start gap-1 text-xs text-[#7f7f7f]">
            <span>对话轮次</span>
            <Tooltip :content="turnsTooltipContent" />
          </div>
          <div>
            <div
              class="text-[30px] leading-none text-white"
              style="font-family: var(--font-num)"
              :title="formatInteger(metrics.turnsTotal)"
            >
              {{ formatCompactInteger(metrics.turnsTotal) }}
            </div>
            <div class="mt-3 text-xs leading-5 text-[#8c8c8c]">
              有效
              <span :title="formatInteger(metrics.validTurnsTotal)">
                {{ formatCompactInteger(metrics.validTurnsTotal) }}
              </span>
              / 异常
              <span :title="formatInteger(metrics.invalidTurnsTotal)">
                {{ formatCompactInteger(metrics.invalidTurnsTotal) }}
              </span>
            </div>
          </div>
        </div>

        <div
          class="min-w-0 border-l border-[#343434] px-4 py-4 flex flex-col justify-between"
        >
          <div class="center-row justify-start gap-1 text-xs text-[#7f7f7f]">
            <span>Token 消耗</span>
            <Tooltip :content="tokensTooltipContent" />
          </div>
          <div>
            <div
              class="truncate text-[30px] leading-none text-white"
              style="font-family: var(--font-num)"
              :title="formatInteger(metrics.requestTokensTotal)"
            >
              {{ formatCompactInteger(metrics.requestTokensTotal) }}
            </div>
            <div class="mt-3 text-xs leading-5 text-[#8c8c8c]">
              Prompt
              <span :title="formatInteger(metrics.promptTokensTotal)">
                {{ formatCompactInteger(metrics.promptTokensTotal) }}
              </span>
            </div>
          </div>
        </div>

        <div
          class="min-w-0 border-l border-[#343434] px-4 py-4 flex flex-col justify-between"
        >
          <div class="center-row justify-start gap-1 text-xs text-[#7f7f7f]">
            <span>价值估算</span>
            <Tooltip :content="costTooltipContent" />
          </div>
          <div>
            <div
              class="truncate text-[30px] leading-none text-white"
              style="font-family: var(--font-num)"
              :title="formatUSD(estimatedTokenCost.total)"
            >
              {{ formatCompactUSD(estimatedTokenCost.total) }}
            </div>
            <div class="mt-3 truncate text-xs leading-5 text-[#8c8c8c]">
              缓存读写
              <span :title="formatUSD(estimatedTokenCost.cacheRead + estimatedTokenCost.cacheWrite)">
                {{ formatCompactUSD(estimatedTokenCost.cacheRead + estimatedTokenCost.cacheWrite) }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <HomeMetricsTrendChart
        :points="metrics.last24Hours"
        :prices="selectedCostPrices"
      />
    </div>
  </div>
</template>

<style scoped></style>
