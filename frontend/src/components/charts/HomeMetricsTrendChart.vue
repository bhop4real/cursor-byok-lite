<script setup>
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from "chart.js";
import { computed, ref } from "vue";
import { Line } from "vue-chartjs";
import { useLocale } from "@/i18n/runtime";
import { formatCompactInteger, formatCompactUSD, formatInteger } from "@/utils/numberFormat";

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip);

const props = defineProps({
  points: {
    type: Array,
    default: () => [],
  },
  prices: {
    type: Object,
    required: true,
  },
});

const METRICS = [
  { id: "tokens", label: "Token", color: "#60a5fa", background: "rgba(96, 165, 250, 0.12)" },
  { id: "cost", label: "费用", color: "#fbbf24", background: "rgba(251, 191, 36, 0.10)" },
  { id: "turns", label: "轮次", color: "#a78bfa", background: "rgba(167, 139, 250, 0.10)" },
];

const TOKEN_SERIES = [
  { field: "inputTokens", label: "普通输入", color: "#60a5fa" },
  { field: "cacheReadTokens", label: "缓存读取", color: "#4ade80" },
  { field: "cacheWriteTokens", label: "缓存写入", color: "#fbbf24" },
  { field: "outputTokens", label: "模型输出", color: "#c084fc" },
];

const { currentLocale } = useLocale();
const activeMetricID = ref("tokens");
const activeMetric = computed(() =>
  METRICS.find((metric) => metric.id === activeMetricID.value) || METRICS[0],
);

function pointCost(point) {
  const prices = props.prices || {};
  return (
    Number(point?.inputTokens || 0) * Number(prices.input || 0)
    + Number(point?.outputTokens || 0) * Number(prices.output || 0)
    + Number(point?.cacheReadTokens || 0) * Number(prices.cacheRead || 0)
    + Number(point?.cacheWriteTokens || 0) * Number(prices.cacheWrite || 0)
  ) / 1_000_000;
}

function pointValue(point) {
  switch (activeMetricID.value) {
    case "cost":
      return pointCost(point);
    case "turns":
      return Number(point?.turnsTotal || 0);
    default:
      return Number(point?.requestTokensTotal || 0);
  }
}

function formatValue(value) {
  switch (activeMetricID.value) {
    case "cost":
      return formatCompactUSD(value);
    case "turns":
      return formatInteger(value);
    default:
      return formatCompactInteger(value);
  }
}

function formatHour(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "--:--";
  }
  return new Intl.DateTimeFormat(currentLocale.value, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatTimestamp(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "时间未知";
  }
  return new Intl.DateTimeFormat(currentLocale.value, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function createDataset({ label, color, background, values, fill = false }) {
  return {
    label: String(label),
    data: values,
    borderColor: color,
    backgroundColor: background || color,
    borderWidth: 1.5,
    pointRadius: 0,
    pointHoverRadius: 3,
    pointHoverBorderWidth: 0,
    tension: 0.32,
    fill,
  };
}

const chartData = computed(() => ({
  labels: props.points.map((point) => formatHour(point.at)),
  datasets: activeMetricID.value === "tokens"
    ? TOKEN_SERIES.map((series) => createDataset({
        label: series.label,
        color: series.color,
        values: props.points.map((point) => Number(point?.[series.field] || 0)),
      }))
    : [createDataset({
        label: activeMetric.value.label,
        color: activeMetric.value.color,
        background: activeMetric.value.background,
        values: props.points.map(pointValue),
        fill: true,
      })],
}));

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: {
    duration: 350,
  },
  interaction: {
    mode: "index",
    intersect: false,
  },
  plugins: {
    legend: {
      display: activeMetricID.value === "tokens",
      align: "start",
      labels: {
        color: "#9a9a9a",
        boxHeight: 2,
        boxWidth: 14,
        padding: 12,
        usePointStyle: true,
        pointStyle: "line",
        font: {
          size: 10,
        },
      },
    },
    tooltip: {
      displayColors: activeMetricID.value === "tokens",
      backgroundColor: "#242424",
      borderColor: "#3b3b3b",
      borderWidth: 1,
      titleColor: "#a3a3a3",
      bodyColor: "#f5f5f5",
      padding: 9,
      callbacks: {
        title(items) {
          const index = items[0]?.dataIndex;
          return formatTimestamp(props.points[index]?.at);
        },
        label(context) {
          const label = context.dataset.label || String(activeMetric.value.label);
          return `${label}：${formatValue(context.parsed.y)}`;
        },
      },
    },
  },
  scales: {
    x: {
      border: {
        display: false,
      },
      grid: {
        display: false,
      },
      ticks: {
        color: "#676767",
        font: {
          size: 10,
        },
        maxTicksLimit: 6,
        maxRotation: 0,
      },
    },
    y: {
      beginAtZero: true,
      border: {
        display: false,
      },
      grid: {
        color: "rgba(255, 255, 255, 0.055)",
        drawTicks: false,
      },
      ticks: {
        color: "#676767",
        font: {
          size: 10,
        },
        padding: 8,
        maxTicksLimit: 4,
        callback: formatValue,
      },
    },
  },
}));
</script>

<template>
  <div class="rounded-[8px] border border-[#343434] bg-[#202020] px-4 pb-3 pt-3">
    <div class="mb-2 flex items-center justify-between gap-4">
      <div class="flex items-baseline gap-2">
        <span class="text-xs text-[#a3a3a3]">过去 24 小时</span>
        <span class="text-[10px] text-[#606060]">按小时汇总</span>
      </div>
      <div class="center-row gap-1 rounded-[5px] border border-[#343434] bg-[#1b1b1b] p-0.5">
        <button
          v-for="metric in METRICS"
          :key="metric.id"
          type="button"
          class="h-[22px] rounded-[4px] px-2 text-[10px] transition-colors duration-150"
          :class="activeMetricID === metric.id ? 'bg-[#343434] text-[#e5e5e5]' : 'text-[#777] hover:text-[#bdbdbd]'"
          @click="activeMetricID = metric.id"
        >
          {{ metric.label }}
        </button>
      </div>
    </div>
    <div class="h-[142px] min-w-0">
      <Line :data="chartData" :options="chartOptions" />
    </div>
  </div>
</template>