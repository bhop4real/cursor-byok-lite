const INTEGER_FORMATTER = new Intl.NumberFormat("en-US");

const COMPACT_UNITS = [
  { value: 1000000000000, suffix: "T" },
  { value: 1000000000, suffix: "B" },
  { value: 1000000, suffix: "M" },
  { value: 1000, suffix: "K" },
];

function normalizeInteger(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return 0;
  }
  return Math.round(number);
}

function trimTrailingZeros(text) {
  return text.replace(/\.0$/, "").replace(/(\.\d*[1-9])0+$/, "$1");
}

export function formatInteger(value) {
  return INTEGER_FORMATTER.format(normalizeInteger(value));
}

export function formatCompactInteger(value) {
  const number = normalizeInteger(value);
  const absNumber = Math.abs(number);
  const unit = COMPACT_UNITS.find(({ value: threshold }) => absNumber >= threshold);

  if (!unit) {
    return formatInteger(number);
  }

  const scaled = number / unit.value;
  const fractionDigits = Math.abs(scaled) < 100 ? 1 : 0;

  return `${trimTrailingZeros(scaled.toFixed(fractionDigits))}${unit.suffix}`;
}

export function formatCompactUSD(value) {
  const amount = Number(value);
  if (!Number.isFinite(amount)) {
    return "$0.00";
  }
  if (amount > 0 && amount < 0.01) {
    return "<$0.01";
  }

  const absAmount = Math.abs(amount);
  const unit = COMPACT_UNITS.find(({ value: threshold }) => absAmount >= threshold);
  if (!unit) {
    return `$${amount.toFixed(2)}`;
  }

  const scaled = amount / unit.value;
  const fractionDigits = Math.abs(scaled) < 10 ? 2 : Math.abs(scaled) < 100 ? 1 : 0;
  return `$${trimTrailingZeros(scaled.toFixed(fractionDigits))}${unit.suffix}`;
}
