QQ group: 1049354347

## 0.0.40-lite

### Features

- Added GPT-5.6 support.
- Added configurable 24-hour usage insights with hourly token, cost, and conversation trends, compact metric formatting, expanded model pricing profiles, and configurable refresh intervals.
- Added Baidu-backed WebSearch with automatic DuckDuckGo fallback when Baidu fails or returns no results.
- Restored image input support for Claude-compatible models.

### Agent and runtime reliability

- Preserved resumable agent state across interruptions, cancellations, reconnects, and restarts, including assistant output, reasoning metadata, and partial tool calls.
- Prevented completed or interrupted tool side effects from being replayed after resume.
- Deduplicated persisted prompt reminders across requests and process restarts.
- Fixed Task model inheritance so subagents honor explicit model selection and otherwise inherit the resolved parent model.
- Hardened conversation forwarding with per-conversation locking, snapshot-based metadata writes, buffered subscriber signals, and lower streaming lock contention.
- Applied disabled reasoning consistently to Anthropic request overrides and removed conflicting adaptive output configuration.
- Added reasoning-disable support for Xiaomi MiMo models to avoid unwanted latency and reasoning-token usage.

### Interface and localization

- Enabled native maximize and resize behavior on Windows.
- Added dynamic localization for footer, update, and tray-menu content in English, Japanese, and Simplified Chinese.
- Improved the Home metrics and Cost Estimate layouts to keep model and context controls readable without card overflow.
- Standardized prompts across Agent, Ask, Debug, Plan, Multitask, Subagent, Compaction, and Commit modes, including automatic response-language selection and deterministic edit and validation guidance.

### Lite distribution

- Removed the bundled advertising subsystem and provider interface for a streamlined lite build.
- Pointed update and release metadata to `bhop4real/cursor-byok-lite`.
- Added PowerShell and batch build entry points for Windows source builds.
- Added verified release archives for Linux amd64, Windows amd64, macOS amd64, and macOS arm64.


