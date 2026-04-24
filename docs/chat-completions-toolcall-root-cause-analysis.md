# Chat/Completions 工具拦截根因与修复分析（2026-04-24）

## 1. 问题现象

在 `chat/completions` 链路中，模型输出了多工具 payload，但运行时只执行了 `Bash`，未执行 `mcp__exa__web_search_exa`。

典型输出（简化）：

```json
{
  "tool_calls": [
    {
      "name": "mcp__exa__web_search_exa",
      "input": {
        "query": "马斯克 最新 动态 2026",
        "num_results": 10
      }
    },
    {
      "name": "Bash",
      "input": {
        "command": "powershell -Command \"Get-PSDrive -Name D | Select-Object Used, Free\""
      }
    }
  ]
}
```

现象是：最终只看到 `Bash(...)` 被执行。

## 2. 分析过程（按排查顺序）

### 2.1 先确认是不是“模型格式不合法”导致的

结论：这确实是一个风险点，但不是唯一根因。

- 如果 payload 内层引号未正确转义，解析器可能只能恢复出部分工具调用。
- 但在“JSON 完整合法”的场景里，`mcp__exa__...` 依旧会被丢弃，说明还有策略层问题。

### 2.2 检查 `chat/completions` 的解析与过滤链路

沿代码路径检查后发现：

1. `normalizeOpenAIChatRequest` 会从请求的 `tools` 中抽取 `toolNames`。
2. `handleNonStream` / `handleStream` 把 `toolNames` 传给工具拦截解析。
3. 解析命中后还会走工具名过滤（allow-list 语义），不在 `toolNames` 里的调用会被拒绝。

因此出现了这个结果：

- 请求里如果只声明了 `Bash`
- 模型输出 `mcp__exa__... + Bash`
- 过滤后只剩 `Bash`

### 2.3 检查流式拦截开关

旧逻辑里：

- `bufferToolContent := len(toolNames) > 0 && featureEnabled`

这意味着当请求未声明工具（或声明不全）时，工具筛流可能根本不启用，造成“未拦截 / 文本泄漏 / 不执行”的问题。

### 2.4 检查 thinking 分支

DeepSeek 常把工具 payload 放在 `response/thinking_content`。  
若只看 `response/content`，会漏拦截。  
本次也补了 `finalText + finalThinking` 双通道检测。

## 3. 根因结论

根因不是单点，而是两层叠加：

1. `chat/completions` 拦截阶段按请求声明工具名做硬过滤，导致“未声明工具”被丢弃。
2. 流式工具筛流启用条件绑定 `len(toolNames) > 0`，在工具声明缺失/不完整时容易直接不拦截。

## 4. 修复方案

### 4.1 `chat/completions` 改为宽松拦截（本次核心）

在 `chat/completions` 链路上不再依赖请求工具清单做硬过滤，允许上游模型输出的有效 standalone `tool_calls` 被拦截并透传执行。

关键调整：

- `internal/adapter/openai/handler_chat.go`
  - 非流式：解析用 `parseToolNames := toolNames[:0]`（宽松模式）
  - 流式：同样宽松解析
  - 流式拦截开关不再依赖 `len(toolNames) > 0`

### 4.2 文本/思考双通道检测

- `internal/format/openai/render_chat.go`
  - 新增 `DetectChatToolCalls(finalText, finalThinking, toolNames)`
  - 优先 text，未命中再看 thinking

### 4.3 解析与筛流兼容增强

- `internal/util/toolcalls_parse.go`
  - 在 allow-list 缺失时保留已解析调用（no-policy fallback）
- `internal/adapter/openai/tool_sieve_core.go`
  - 修正关键字结构分类，避免误走 JSON 分支

### 4.4 保留 Responses 的安全边界

`responses` 仍保留 `tool_choice=none` 的强约束，不因“宽松拦截”回归：

- `internal/adapter/openai/responses_handler.go`
- `internal/adapter/openai/responses_stream_runtime_core.go`

## 5. 验证过程

新增/更新了以下回归方向：

1. 未声明工具也能在 `chat/completions` 被拦截为 `tool_calls`
2. `mcp__exa__... + Bash` 混合 payload 两者都保留
3. `response/thinking_content` 的 payload 能被命中
4. `TOOL_CALL_HISTORY` 文本块可恢复工具调用
5. `tool_choice=none` 在 `responses` 继续严格阻断

执行结果：

```bash
go test ./internal/util ./internal/adapter/openai -count=1
```

通过。

## 6. 结果与边界

修复后，`chat/completions` 场景下，类似以下 payload 不再只剩 `Bash`：

```json
{
  "tool_calls": [
    { "name": "mcp__exa__web_search_exa", "input": { "query": "马斯克", "num_results": 5 } },
    { "name": "Bash", "input": { "command": "date +%Y-%m-%d" } }
  ]
}
```

注意边界：

- 若模型输出本身是严重损坏 JSON（尤其字符串引号未闭合/未转义），解析器仍可能出现部分恢复，这是输入质量问题，不是本次策略问题。

## 7. 相关提交

- `6f32c0b` `fix: allow chat tool-call interception beyond declared tool names`
- `3214ed6` `fix: harden tool payload parsing across chat and responses`
