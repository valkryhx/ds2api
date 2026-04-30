# DeepSeek pro-think 模型工具调用格式分析及增强设计

## 1. 问题陈述

`deepseek-v4-pro-think` 模型在需要工具调用时，经常违反 `工具调用规范.md` 约定的纯 JSON 输出格式。模型会输出额外的思考内容（通常以 `thinking` 标签或类似标记包裹），导致现有解析器无法稳定提取 `tool_calls` 结构，从而需要频繁更新拦截规则。

相对的，`deepseek-v4-flash` 系列模型表现较好，基本遵循约定格式。

## 2. 现有代码分析

### 2.1 已有的 thinking 处理

- `internal/util/toolcalls_helpers.go`：
  - `looksLikeToolExampleContext`：检测到 "thinking" 子串时增加示例计数（可能误将真实调用当作示例）。
  - `extractTrailingStandaloneJSONObjectCandidate`：在文本中首次出现 "thinking" 后，尝试提取后面的第一个 `{}` 作为工具调用候选。
  - `looksLikeToolExamplePrefix`：同样检测 "thinking" 以决定是否跳过某些候选。
- `internal/util/toolcalls_parse.go`：无专门针对 `thinking` 标签的清理步骤。

### 2.2 局限性

- 仅通过子串 `"thinking"` 匹配，未考虑大小写、不同标签形式（`<thinking>`、`/thinking`、`[THINK]` 等）。
- 没有主动移除思考内容，而是依赖后续 JSON 解析的容错。当思考内容包含 JSON 片段时可能导致解析错误。
- 测试覆盖不足（`toolcalls_test.go` 只有一个简单的 thinking 测试案例）。
- 对于复杂的输出（如多行思考 + 多个工具调用 + 嵌套标签）可能失效。

## 3. 设计方案

### 3.1 新增思考内容剥离函数

在 `internal/util/toolcalls_helpers.go` 中添加：

```go
// stripThinkingBlocks removes common thinking/reasoning blocks from model output.
// It handles:
//   - <thinking>...</thinking>
//   - ```think ... ``` or ```thinking ... ```
//   - [THINK]...[/THINK]
//   - Lines starting with "Thinking:" or "Reasoning:" (optional)
func stripThinkingBlocks(s string) string {
    // Implementation using regexp
}
```

匹配模式（大小写不敏感）：
- `(?is)<thinking>.*?</thinking>`
- `(?s)```think\s*\n.*?\n``` `
- `(?s)```thinking\s*\n.*?\n``` `
- `(?is)\[THINK\].*?\[/THINK\]`

剥离后返回剩余文本。

### 3.2 集成到解析流程

在 `ParseStandaloneToolCallsDetailed` 中，**在生成 candidates 之前**调用 `stripThinkingBlocks` 预处理输入文本：

```go
func ParseStandaloneToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
    trimmed := strings.TrimSpace(text)
    if trimmed == "" {
        return result
    }
    // NEW: strip thinking blocks first
    cleaned := stripThinkingBlocks(trimmed)
    // ... rest of the logic uses cleaned
}
```

### 3.3 增强 `extractTrailingStandaloneJSONObjectCandidate`

在该函数开头也调用 `stripThinkingBlocks`，确保提取候选时不受残留思考标记影响。

### 3.4 调整 Prompt（可选但推荐）

针对 `deepseek-v4-pro-think` 模型，在 `injectToolPrompt` 中追加特定指令：

```text
For deepseek-v4-pro-think: if you need to think, put your reasoning inside <thinking>...</thinking> tags, then output ONLY the raw JSON tool call.
```

### 3.5 增加单元测试

在 `toolcalls_test.go` 中添加以下测试用例：

- 纯 `<thinking>` 标签 + 后续有效 JSON
- 多行 `thinking` 代码块 + JSON
- 混合标签（如 `<thinking>` 内部含代码块）
- 无 JSON 只有思考（应返回空）
- 多个工具调用 + 思考
- 真实 pro-think 输出样例

## 4. 实施计划

1. **代码实现**
   - 实现 `stripThinkingBlocks`
   - 修改 `ParseStandaloneToolCallsDetailed` 调用清理函数
   - 修改 `extractTrailingStandaloneJSONObjectCandidate` 调用清理函数
   - 修改 `injectToolPrompt` 针对 pro-think 添加指令

2. **测试**
   - 编写上述单元测试
   - 运行所有测试确保无回归

3. **验证**
   - 部署到测试环境观察解析成功率
   - 监控日志

4. **文档更新**
   - 更新 `工具调用规范.md`

## 5. 风险与缓解

- **过剥离风险**：只剥离带有标签结构的完整块，而非简单子串。
- **性能影响**：可接受。
- **与其他解析器兼容性**：无影响。

## 6. 成功标准

- 单元测试覆盖至少 5 种 pro-think 典型输出格式
- 生产环境中 pro-think 模型工具调用解析失败率降低 80% 以上
- 无新增解析器回归

## 7. 后续工作

- 收集更多真实样本，建立持续回归测试集。
