QQ group: 1049354347

## 0.0.40.1-lite

### Runtime reliability

- Recovered recurring oversized, truncated OpenAI-compatible terminal frames when the completed status is carried by either the SSE event name or the JSON envelope, while preserving the existing retry boundary before semantic output escapes.
- Kept provider stream diagnostics structural at the user-visible error boundary so truncated payloads cannot expose prompt instructions, message history, tool arguments, or response content.
- Added bounded, idempotent Write and PatchEdit completion with read-only recovery so delayed, duplicated, or missing client results cannot replay mutations or leave operations pending indefinitely.
- Hardened actor, reconnect, shell-recovery, and checkpoint flows so interrupted work settles once and resumes only its originating provider pass.
- Preserved current-pass tool contracts and complete AskQuestion selections across replay and continuation.

### Context and provider projection

- Added an isolated compact-context and tool-projection path for newly created conversations, with canonical history preserved and baseline fallback on translation failure.
- Moved provider projection ahead of compaction decisions and aligned automatic summarization, checkpoints, and token display with the effective provider-facing request.
- Prevented stale usage samples and duplicated capability reminders from retriggering compaction or inflating projected prompts.

### Configuration, diagnostics, and interface

- Added lossless settings patches and hot reload while preserving unknown YAML fields, omitted secrets, and restart-scoped behavior.
- Expanded editable runtime, routing, response-language, update, listener, and Home metrics settings with validation and restart guidance.
- Added request, provider, tool-stage, recovery, and offline profiling diagnostics for investigating latency and continuation failures.
- Completed updater, validation, and native-window localization across English, Japanese, and Simplified Chinese.

### Release compatibility

- Added four-component version ordering so `0.0.40.1-lite` is correctly recognized as newer than `0.0.40-lite` without claiming the upstream `0.0.41` version line.


