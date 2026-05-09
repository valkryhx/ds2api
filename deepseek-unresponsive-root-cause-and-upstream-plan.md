# DeepSeek no-response root cause and upstream optimization plan

Date: 2026-05-09

## Scope

This note records the root cause of the latest Claude Code -> ds2api -> DeepSeek Web test where Claude Code showed DSML tool-call output, then later appeared to stop responding after the user repeatedly sent "继续".

It also records the upstream ds2api strategy that should guide the next optimization pass in this fork.

## Observed symptom

During a Claude Code session routed through this project's OpenAI-compatible endpoint:

- Tool calls were successfully emitted for commands such as `Bash`, `Read`, and multiple file reads.
- After a long running session, Claude Code displayed a long wait such as `Sautéed for 16m 10s`.
- The user sent `继续` multiple times.
- Claude Code did not produce a useful visible response.
- DeepSeek Web showed a separate title-generation request/result like:

```json
{"title":"Continue coding session"}
```

At first glance this looked like one of these problems:

- DeepSeek account rate limit.
- ds2api sending the continuation request as a title-generation request by mistake.
- Streaming/tool-call parser deadlock.
- DeepSeek Web not responding.

The logs point to a different root cause.

## Evidence from local captures

Relevant local capture files:

- `logs/dev_captures/openai_inbound.jsonl`
- `logs/dev_captures/deepseek_completion.jsonl`
- `logs/dev_captures/openai_tool_use.jsonl`

The important DeepSeek completion captures are:

- `cap_e929b027f90647379f42f315ce4f2c15`
- `cap_8bd4405d2e7046fb8acb402034b6b6d3`
- `cap_e7ca071ad55d4486b1f93e7c35e47a8e`
- `cap_b9be5ffcfb60412da92457e90ffe2799`
- `cap_8f05a9ea5539436aaeb4a1b28bd61b4a`

They all have HTTP status `200`, `model_type:"expert"`, and DeepSeek returns this SSE shape:

```text
event: hint
data: {"type":"error","content":"内容超长，请删减后再试","clear_response":true,"finish_reason":"input_exceeds_limit"}

event: close
data: {"click_behavior":"none","auto_resume":false}
```

This is not a normal model content stream and not a normal HTTP error. It is an SSE hint event embedded in an HTTP 200 response.

The title request is real but separate. `openai_inbound.jsonl` contains requests whose first message asks:

```text
Generate a concise, sentence-case title (3-7 words) that captures the main topic or goal of this coding session.
Return JSON with a single "title" field.
```

Those title requests are normal Claude Code side traffic. They are not the failing continuation request. The main Claude Code requests are distinguishable by the large first message beginning with:

```text
You are Claude Code, Anthropic's official CLI for Claude.
You are an interactive agent that helps users with software engineering tasks.
```

## Root cause

The immediate root cause is `input_exceeds_limit`: the OpenAI-compatible request sent by Claude Code became too large for the DeepSeek Web completion endpoint.

The visible "no response" behavior is caused by a second bug in this fork: the local SSE parser does not recognize DeepSeek's `event: hint` error envelope. It only parses lines beginning with `data:` and only handles these error shapes:

- `data: {"error": ...}`
- `data: {"code":"content_filter", ...}`
- normal `response/content`, `response/thinking_content`, fragment, or `FINISHED` payloads
- `data: [DONE]`

Current local code path:

- `internal/sse/parser.go`
  - `ParseDeepSeekSSELine` ignores `event:` lines.
  - It parses the following `data:` JSON, but the JSON does not have an `error` field.
- `internal/sse/line.go`
  - `ParseDeepSeekContentLine` does not inspect `type:"error"`, `content`, `clear_response`, or `finish_reason:"input_exceeds_limit"`.
  - The hint payload has no `v`, so `ParseSSEChunkForContent` returns no parts and no terminal error.
- `internal/stream/engine.go`
  - Since no content is seen, stream keepalive/no-content timeout behavior can make the client experience look like a hang.
- `internal/adapter/openai/chat_stream_runtime.go`
  - If no parser error is surfaced, finalization can emit a generic empty/stop result instead of a clear context-limit error.

So the user-facing symptom is misleading:

- DeepSeek did respond.
- The response was an HTTP 200 SSE error hint.
- ds2api did not translate that hint into an OpenAI/Claude-compatible error frame.
- Repeated `继续` only resent another oversized full-history prompt, so it hit the same `input_exceeds_limit` again.

## Why "continue" did not recover

Claude Code sends the current conversation state back through the OpenAI-compatible endpoint. In this fork, the OpenAI path builds a full prompt from the entire request history and sends it directly to DeepSeek:

```json
{
  "chat_session_id": "...",
  "model_type": "expert",
  "parent_message_id": null,
  "prompt": "...full Claude Code context...",
  "ref_file_ids": [],
  "thinking_enabled": true,
  "search_enabled": false
}
```

When the history already exceeds the DeepSeek Web single-message/input limit, a user message like `继续` does not shrink the prompt. It appends another turn to an already oversized context. The retry therefore repeats the same failure.

This also explains why the DeepSeek Web title display was confusing. Claude Code separately asks for short session titles, and those requests can still succeed because they are small. That does not mean the main continuation request was transformed into a title request.

## Relation to rate limits

The captured failure is not a rate-limit failure.

The evidence differs from a rate-limit path:

- HTTP status is `200`, not `429`.
- The SSE payload says `finish_reason:"input_exceeds_limit"`.
- The message is `内容超长，请删减后再试`.
- It does not say the account hit a usage or rate limit.

Rate-limit handling may still be needed elsewhere, but it is not the cause of this specific no-response incident.

## Upstream solution

The upstream project at `D:\git_repos\origin_ds2api\ds2api` solved this class of issue mainly by avoiding direct full-history inline prompts.

The key upstream feature is `current_input_file`.

Relevant upstream files:

- `internal/httpapi/openai/history/current_input_file.go`
- `internal/promptcompat/history_transcript.go`
- `internal/promptcompat/standard_request.go`
- `internal/completionruntime/nonstream.go`
- `docs/prompt-compatibility.md`
- `README.MD`

Upstream behavior:

1. `current_input_file` is enabled by default.
2. `current_input_file.min_chars` defaults to `0`.
3. When triggered, upstream serializes the normalized full conversation into an uploaded file named `DS2API_HISTORY.txt`.
4. The live prompt is replaced by a short continuation message:

```text
Continue from the latest state in the attached DS2API_HISTORY.txt context. Treat it as the current working state and answer the latest user request directly.
```

5. The uploaded file ID is prepended to `ref_file_ids`.
6. The completion payload still uses the resolved model type (`default`, `expert`, or `vision`) for both file upload and completion.
7. Token accounting keeps the pre-split full-context semantics via `PromptTokenText`, so client-facing usage does not incorrectly pretend the real context became tiny.

The upstream `DS2API_HISTORY.txt` transcript format is simple and stable:

```text
# DS2API_HISTORY.txt
Prior conversation history and tool progress.

=== 1. SYSTEM ===
...

=== 2. USER ===
...

=== 3. ASSISTANT ===
...

=== 4. TOOL ===
[name=Read tool_call_id=...]
...
```

This is not proof that DeepSeek Web's native single prompt context is 1M tokens. It is an engineering workaround: move large history into a DeepSeek reference file and keep the live prompt small.

Upstream public notes also mention "拓展1M完整上下文（解决deepseek单消息限制）", but the important implementation fact is file-based context externalization, not a hard native token-window guarantee.

## Upstream streaming/latency improvements

Upstream also made streaming improvements that are relevant to the user's earlier "write/tool call stalls" concern, but these solve a different layer.

Relevant upstream commits/PRs:

- `706e68d` increased large-context stream tolerance:
  - `StreamIdleTimeout`: `90` -> `300`
  - `MaxKeepaliveCount`: `10` -> `40`
- `d407ccb` / PR #406 optimized streaming TTFT and buffering:
  - reader/scanner runs in a goroutine
  - parsed output is sent through a channel
  - first chunk flushes immediately
  - accumulation thresholds are much smaller
  - Vercel JS stream emitter flush thresholds were reduced
  - scanner max line size was raised for very long SSE lines

This fork already has some timeout values raised:

- `internal/deepseek/constants.go`
  - `StreamIdleTimeout = 300`
  - `MaxKeepaliveCount = 40`

But this fork does not yet have upstream's full `current_input_file` context offloading pipeline, and its local `internal/sse/stream.go` is still a simpler reader loop. The context-limit incident therefore remains reproducible.

## Current fork gaps

This fork currently has no `current_input_file` or `DS2API_HISTORY.txt` implementation:

```text
rg "current_input_file|DS2API_HISTORY|history_split|PromptTokenText" .
# no matches
```

Primary gaps:

1. No config fields for enabling current-input file offload.
2. No DeepSeek file upload path wired into the completion runtime for OpenAI/Claude/Gemini requests.
3. No transcript serializer for full chat history and tool results.
4. No `StandardRequest.PromptTokenText` equivalent.
5. No parser handling for DeepSeek SSE hint errors such as `input_exceeds_limit`.
6. Stream parser does not carry SSE event names, so `event: hint` context is lost.
7. No regression tests for `event: hint` + `finish_reason:"input_exceeds_limit"`.
8. No fallback policy for oversized prompt when file upload fails.

## Recommended next optimization plan

### Phase 1: Make the error visible

Goal: the client must see a clear error instead of an apparent hang.

Implement DeepSeek hint-error parsing:

- Extend SSE parsing to remember the latest `event:` name or infer hint errors from the `data:` payload itself.
- Treat payloads like this as terminal errors:

```json
{
  "type": "error",
  "content": "内容超长，请删减后再试",
  "clear_response": true,
  "finish_reason": "input_exceeds_limit"
}
```

- Map `input_exceeds_limit` to a stable internal code, for example:
  - `upstream_input_exceeds_limit`
  - HTTP `413 Payload Too Large` for non-stream OpenAI-compatible responses
  - stream error frame for Claude/OpenAI/Responses streaming paths

Suggested files to inspect/change:

- `internal/sse/parser.go`
- `internal/sse/line.go`
- `internal/sse/consumer.go`
- `internal/stream/engine.go`
- `internal/adapter/openai/chat_stream_runtime.go`
- `internal/adapter/openai/responses_stream_runtime_core.go`
- `internal/adapter/claude/stream_runtime_core.go`

Regression tests:

- `data: {"type":"error","content":"内容超长，请删减后再试","clear_response":true,"finish_reason":"input_exceeds_limit"}` returns `LineResult{Stop:true, ErrorMessage:..., FinishReason:"input_exceeds_limit"}`.
- OpenAI non-stream returns an OpenAI error object instead of an empty assistant message.
- OpenAI chat stream emits an error terminal chunk and `[DONE]`.
- Responses stream emits `response.failed` with code `upstream_input_exceeds_limit`.
- Claude stream emits an Anthropic-compatible error event.

### Phase 2: Port upstream `current_input_file`

Goal: prevent repeated `input_exceeds_limit` on long Claude Code sessions.

Port the upstream design, adapted to this fork's module names.

Implementation steps:

1. Add config:

```json
"current_input_file": {
  "enabled": true,
  "min_chars": 0
}
```

2. Add store accessors and validation:

- enabled defaults to `true`
- `min_chars` defaults to `0`
- upper bound can follow upstream's `100000000`

3. Add a transcript serializer:

- `DS2API_HISTORY.txt`
- numbered role sections
- include system/user/assistant/tool messages
- include tool name and `tool_call_id` for tool result turns
- preserve DSML/tool history markers where already present

4. Add DeepSeek upload support if missing or incomplete:

- filename: `DS2API_HISTORY.txt`
- content type: `text/plain; charset=utf-8`
- purpose: `assistants`
- model type: resolved from requested model (`default`, `expert`, `vision`)

5. Rewrite the standardized request before completion:

- upload transcript
- set `messages` to one short continuation user message
- prepend uploaded file ID to `ref_file_ids`
- rebuild final prompt and tool names
- keep usage accounting based on full transcript + short live prompt

6. Apply globally before `CreateSession`/`GetPow`/`CallCompletion`:

- OpenAI `/v1/chat/completions`
- OpenAI `/v1/responses`
- Claude `/anthropic/v1/messages`
- Gemini compatibility path if it normalizes into the same request abstraction

7. Keep behavior configurable:

- if disabled, preserve current direct prompt behavior
- if upload fails, return a clear error rather than silently sending the oversized inline prompt again

Suggested upstream code to use as reference:

- `D:\git_repos\origin_ds2api\ds2api\internal\httpapi\openai\history\current_input_file.go`
- `D:\git_repos\origin_ds2api\ds2api\internal\promptcompat\history_transcript.go`
- `D:\git_repos\origin_ds2api\ds2api\internal\promptcompat\standard_request.go`
- `D:\git_repos\origin_ds2api\ds2api\internal\completionruntime\nonstream.go`

### Phase 3: Align token accounting

Goal: clients should not underestimate context after file offload.

Upstream added `PromptTokenText` to preserve full-context accounting after the live prompt is shortened. This fork should add the same concept to its normalized request structure.

Rules:

- Before `current_input_file`, `PromptTokenText` should equal the final prompt text.
- After `current_input_file`, `PromptTokenText` should equal:

```text
DS2API_HISTORY transcript + "\n" + shortened live prompt
```

- Usage builders should count `PromptTokenText`, not only the shortened `FinalPrompt`.

Suggested tests:

- usage does not collapse to a tiny prompt token count after context offload
- uploaded file token estimate is included
- chat/responses/Claude/Gemini surfaces produce consistent accounting

### Phase 4: Port upstream streaming granularity carefully

Goal: reduce perceived stalls for tool calls and long write payloads.

This is separate from the context-limit fix, but it should be considered next because Claude Code write/read tool output can generate very long SSE lines or long buffered DSML blocks.

Compare this fork's `internal/sse/stream.go` with upstream's newer version:

- upstream uses `bufio.Scanner` with explicit max line size
- scanner runs in its own goroutine
- parsed lines are accumulated with smaller thresholds
- first flush is immediate
- newline can trigger flush
- `TrimContinuationOverlapFromBuilder` avoids repeated builder string copies

When porting, keep this fork's recent DSML/tool-sieve changes intact. Do not replace tool parsing wholesale unless tests show equivalence.

Suggested tests:

- very long single SSE line with a `Write.content` payload is preserved
- first visible token flushes without waiting for a large buffer
- DSML partial wrappers do not leak as plain text
- final complete tool call still emits `finish_reason:"tool_calls"`

## Implementation order

The safest order is:

1. Add `input_exceeds_limit` parser/renderer tests and fix error propagation.
2. Add config and transcript serializer tests.
3. Add upload abstraction and current-input-file rewrite tests.
4. Wire rewrite into OpenAI chat first.
5. Extend to OpenAI Responses, Claude, and Gemini.
6. Add token accounting preservation.
7. Port streaming granularity improvements.
8. Run focused parser/adapter tests, then full Go and Node unit suites.

## Verification commands for next implementation

Use focused loops first:

```powershell
go test ./internal/sse ./internal/stream -count=1
go test ./internal/adapter/openai -count=1
go test ./internal/adapter/claude -count=1
go test ./internal/util -count=1
```

Then run the baseline:

```powershell
go test ./...
./tests/scripts/run-unit-node.sh
```

For parser/stream changes, also add fixture-style regressions if the compatibility harness is still in use:

- `tests/compat/fixtures/`
- `tests/compat/expected/`

## Expected outcome after optimization

After Phase 1, this exact failure should no longer look like "DeepSeek not responding". Claude Code should receive an explicit upstream context-limit error.

After Phase 2, long Claude Code sessions should avoid repeatedly sending the full session inline and should instead send:

- one uploaded `DS2API_HISTORY.txt` reference file
- one short live continuation prompt

After Phase 3, client usage/token accounting should still reflect the real full context rather than only the short continuation prompt.

After Phase 4, large tool-call/write streams should become less likely to appear stalled because smaller stream chunks flush earlier and long SSE lines are handled more deliberately.
