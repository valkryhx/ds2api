package util

import (
	"strings"
	"testing"
)

func TestParseToolCalls(t *testing.T) {
	text := `prefix {"tool_calls":[{"name":"search","input":{"q":"golang"}}]} suffix`
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "search" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["q"] != "golang" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseToolCallsFromFencedJSON(t *testing.T) {
	text := "I will call tools now\n```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"news\"}}]}\n```"
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 0 {
		t.Fatalf("expected fenced tool_call example to be ignored, got %#v", calls)
	}
}

func TestParseToolCallsWithFunctionArgumentsString(t *testing.T) {
	text := `{"tool_calls":[{"function":{"name":"get_weather","arguments":"{\"city\":\"beijing\"}"}}]}`
	calls := ParseToolCalls(text, []string{"get_weather"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["city"] != "beijing" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseToolCallsRejectsUnknownToolName(t *testing.T) {
	text := `{"tool_calls":[{"name":"unknown","input":{}}]}`
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 0 {
		t.Fatalf("expected unknown tool to be rejected, got %#v", calls)
	}
}

func TestParseToolCallsAllowsCaseInsensitiveToolNameAndCanonicalizes(t *testing.T) {
	text := `{"tool_calls":[{"name":"Bash","input":{"command":"ls -al"}}]}`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
}

func TestParseToolCallsDetailedMarksPolicyRejection(t *testing.T) {
	text := `{"tool_calls":[{"name":"unknown","input":{}}]}`
	res := ParseToolCallsDetailed(text, []string{"search"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true, got %#v", res)
	}
	if !res.RejectedByPolicy {
		t.Fatalf("expected RejectedByPolicy=true, got %#v", res)
	}
	if len(res.Calls) != 0 {
		t.Fatalf("expected no calls after policy rejection, got %#v", res.Calls)
	}
}

func TestParseToolCallsDetailedRejectsWhenAllowListEmpty(t *testing.T) {
	text := `{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`
	res := ParseToolCallsDetailed(text, nil)
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true, got %#v", res)
	}
	if !res.RejectedByPolicy {
		t.Fatalf("expected RejectedByPolicy=true, got %#v", res)
	}
	if len(res.Calls) != 0 {
		t.Fatalf("expected no calls when allow-list is empty, got %#v", res.Calls)
	}
}

func TestFormatOpenAIToolCalls(t *testing.T) {
	formatted := FormatOpenAIToolCalls([]ParsedToolCall{{Name: "search", Input: map[string]any{"q": "x"}}})
	if len(formatted) != 1 {
		t.Fatalf("expected 1, got %d", len(formatted))
	}
	fn, _ := formatted[0]["function"].(map[string]any)
	if fn["name"] != "search" {
		t.Fatalf("unexpected function name: %#v", fn)
	}
}

func TestParseStandaloneToolCallsOnlyMatchesStandalonePayload(t *testing.T) {
	mixed := `这里是示例：{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`
	if calls := ParseStandaloneToolCalls(mixed, []string{"search"}); len(calls) != 0 {
		t.Fatalf("expected standalone parser to ignore mixed prose, got %#v", calls)
	}

	standalone := `{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`
	calls := ParseStandaloneToolCalls(standalone, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected standalone parser to match, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsAllowsStandaloneFencedPayload(t *testing.T) {
	fenced := "```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"go\"}}]}\n```"
	calls := ParseStandaloneToolCalls(fenced, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected standalone fenced payload to be parsed, got %#v", calls)
	}
	if calls[0].Name != "search" {
		t.Fatalf("expected tool name search, got %#v", calls[0].Name)
	}
}

func TestParseStandaloneToolCallsIgnoresFencedCodeBlockWithProse(t *testing.T) {
	fenced := "这是示例，不要执行：\n```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"go\"}}]}\n```"
	if calls := ParseStandaloneToolCalls(fenced, []string{"search"}); len(calls) != 0 {
		t.Fatalf("expected fenced example with prose to be ignored, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsAllowsTrailingPayloadAfterProse(t *testing.T) {
	text := "我将启动完整的验证流程：先运行单元测试检查基础功能，再启动服务执行端到端测试。\n\n" +
		"{\"tool_calls\": [{\"name\": \"Bash\", \"description\": \"检查pytest是否可用\", \"command\": \"pytest --version\"}]}"
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected trailing payload to be parsed, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "pytest --version" {
		t.Fatalf("expected implicit command arg, got %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsSupportsToolUseLabelStyle(t *testing.T) {
	text := "我来搜索 DeepSeek V4 的测评结果。\n\nTool use:\nmcp__exa__web_search_exa\n{\"query\":\"DeepSeek V4 测评\"}"
	calls := ParseStandaloneToolCalls(text, []string{"mcp__exa__web_search_exa"})
	if len(calls) != 1 {
		t.Fatalf("expected tool use label style to be parsed, got %#v", calls)
	}
	if calls[0].Name != "mcp__exa__web_search_exa" {
		t.Fatalf("unexpected tool name: %#v", calls[0].Name)
	}
	if calls[0].Input["query"] != "DeepSeek V4 测评" {
		t.Fatalf("unexpected query arg: %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsIgnoresAlreadyCalledMCPHistoryWhenAllowListMissing(t *testing.T) {
	text := `[TOOL_CALL_HISTORY]
status: already_called
origin: assistant
not_user_input: true
tool_call_id: call_2be3b3b84d2e45e2b9e300e40af39164
function.name: mcp__exa__web_search_exa
function.arguments: {"query":"马斯克","num_results":10}
[/TOOL_CALL_HISTORY]`
	calls := ParseStandaloneToolCalls(text, nil)
	if len(calls) != 0 {
		t.Fatalf("expected already_called history block to be ignored, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsAllowsNonMCPWhenAllowListMissing(t *testing.T) {
	text := `{"tool_calls":[{"name":"Bash","input":{"command":"date +%Y-%m-%d"}}]}`
	calls := ParseStandaloneToolCalls(text, nil)
	if len(calls) != 1 {
		t.Fatalf("expected calls to pass when allow-list is missing, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected tool name: %#v", calls[0].Name)
	}
	if calls[0].Input["command"] != "date +%Y-%m-%d" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsIgnoresTrailingPayloadWhenPrefixIsExample(t *testing.T) {
	text := "下面是示例：\n" +
		"{\"tool_calls\": [{\"name\": \"search\", \"input\": {\"q\": \"go\"}}]}"
	if calls := ParseStandaloneToolCalls(text, []string{"search"}); len(calls) != 0 {
		t.Fatalf("expected example prefix payload to be ignored, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsRecoversTruncatedShortPayload(t *testing.T) {
	text := `{"tool_calls":[{"name":"Bash","input":{"command":"pytest --version"}}`
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected truncated payload to be recovered, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "pytest --version" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsRepairsTrailingCommas(t *testing.T) {
	text := `{"tool_calls":[{"name":"Bash","input":{"command":"echo hi",}},]}`
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected trailing commas to be repaired, got %#v", calls)
	}
	if calls[0].Input["command"] != "echo hi" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseToolCallsAllowsQualifiedToolName(t *testing.T) {
	text := `{"tool_calls":[{"name":"mcp.search_web","input":{"q":"golang"}}]}`
	calls := ParseToolCalls(text, []string{"search_web"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "search_web" {
		t.Fatalf("expected canonical tool name search_web, got %q", calls[0].Name)
	}
}

func TestParseToolCallsAllowsPunctuationVariantToolName(t *testing.T) {
	text := `{"tool_calls":[{"name":"read-file","input":{"path":"README.md"}}]}`
	calls := ParseToolCalls(text, []string{"read_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("expected canonical tool name read_file, got %q", calls[0].Name)
	}
}

func TestParseToolCallsSupportsClaudeXMLToolCall(t *testing.T) {
	text := `<tool_call><tool_name>Bash</tool_name><parameters><command>pwd</command><description>show cwd</description></parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsDetailedMarksXMLToolCallSyntax(t *testing.T) {
	text := `<tool_call><tool_name>Bash</tool_name><parameters><command>pwd</command></parameters></tool_call>`
	res := ParseToolCallsDetailed(text, []string{"bash"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true, got %#v", res)
	}
	if len(res.Calls) != 1 {
		t.Fatalf("expected one parsed call, got %#v", res)
	}
}

func TestParseToolCallsSupportsClaudeXMLJSONToolCall(t *testing.T) {
	text := `<tool_call>{"tool":"Bash","params":{"command":"pwd","description":"show cwd"}}</tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsFunctionCallTagStyle(t *testing.T) {
	text := `<function_call>Bash</function_call><function parameter name="command">ls -la</function parameter><function parameter name="description">list</function parameter>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "ls -la" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsAntmlFunctionCallStyle(t *testing.T) {
	text := `<antml:function_calls><antml:function_call name="Bash">{"command":"pwd","description":"x"}</antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsDSMLShell(t *testing.T) {
	text := `<|DSML|tool_calls><|DSML|invoke name="Bash"><|DSML|parameter name="command"><![CDATA[pwd]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 DSML call, got %#v", calls)
	}
	if calls[0].Name != "Bash" || calls[0].Input["command"] != "pwd" {
		t.Fatalf("unexpected DSML parse result: %#v", calls[0])
	}
}

func TestParseToolCallsSupportsAntmlArgumentStyle(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="1" name="Bash"><antml:argument name="command">pwd</antml:argument><antml:argument name="description">x</antml:argument></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsInvokeFunctionCallStyle(t *testing.T) {
	text := `<function_calls><invoke name="Bash"><parameter name="command">pwd</parameter><parameter name="description">d</parameter></invoke></function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsInvokeCommandArray(t *testing.T) {
	text := `<invoke name="shell"><parameter name="command">["powershell.exe","-Command","Write-Host 'hello'"]</parameter></invoke>`
	calls := ParseToolCalls(text, []string{"shell"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	cmd, ok := calls[0].Input["command"].([]any)
	if !ok {
		t.Fatalf("expected command parsed as []any, got %T %#v", calls[0].Input["command"], calls[0].Input["command"])
	}
	if len(cmd) != 3 {
		t.Fatalf("expected 3 command tokens, got %#v", cmd)
	}
	if cmd[0] != "powershell.exe" || cmd[1] != "-Command" {
		t.Fatalf("unexpected command tokens: %#v", cmd)
	}
}

func TestParseStandaloneToolCallsSupportsInvokeArrayAcrossMixedProse(t *testing.T) {
	text := `<invoke name="shell"><parameter name="command">["powershell.exe","-Command","git -C D:/git_repos/ds2api log --oneline -20"]</parameter></invoke>

I notice parser issues:

<invoke name="shell"><parameter name="command">["powershell.exe","-Command","Get-Content -Path 'D:\\git_repos\\ds2api\\.git\\logs\\HEAD' -Tail 20"]</parameter></invoke>`
	calls := ParseStandaloneToolCalls(text, []string{"shell_command"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	cmd0, ok := calls[0].Input["command"].([]any)
	if !ok || len(cmd0) != 3 {
		t.Fatalf("expected first command parsed as []any, got %#v", calls[0].Input["command"])
	}
}

func TestParseToolCallsSupportsMultipleInvokeFunctionCalls(t *testing.T) {
	text := `<function_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke><invoke name="Read"><parameter name="path">README.MD</parameter></invoke></function_calls>`
	calls := ParseToolCalls(text, []string{"bash", "read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	if calls[0].Name != "bash" || calls[1].Name != "read" {
		t.Fatalf("expected canonical names [bash read], got %#v", calls)
	}
}

func TestParseStandaloneToolCallsSupportsFunctionWrapperStyle(t *testing.T) {
	text := `<function><name>shell</name><parameter name="command">["pwd"]</parameter><parameter name="workdir">"D:\\git_repos\\ds2api"</parameter></function>`
	calls := ParseStandaloneToolCalls(text, []string{"shell_command"})
	if len(calls) != 1 {
		t.Fatalf("expected one parsed call, got %#v", calls)
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected tool name shell, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] == nil {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsBypassesAllowListWhenNotToolChoiceNone(t *testing.T) {
	text := `<invoke name="shell"><parameter name="command">["pwd"]</parameter></invoke>`
	calls := ParseStandaloneToolCalls(text, []string{"shell_command"})
	if len(calls) != 1 {
		t.Fatalf("expected parsed call to bypass allow-list mismatch, got %#v", calls)
	}
	if calls[0].Name != "shell" {
		t.Fatalf("expected original name shell, got %#v", calls[0].Name)
	}
}

func TestParseStandaloneToolCallsKeepsToolChoiceNoneBlock(t *testing.T) {
	text := `<invoke name="shell"><parameter name="command">["pwd"]</parameter></invoke>`
	calls := ParseStandaloneToolCalls(text, []string{"__tool_choice_none_block__"})
	if len(calls) != 0 {
		t.Fatalf("expected no calls for tool_choice none sentinel, got %#v", calls)
	}
}

func TestParseToolCallsSupportsNestedToolTagStyle(t *testing.T) {
	text := `<tool_call><tool name="Bash"><command>pwd</command><description>show cwd</description></tool></tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsSupportsToolCWithNamedParameterAttribute(t *testing.T) {
	text := `<tool_calls><tool_c name="Read"><parameter name="file_path" string="true">D:\git_codes\google_adk_helloworld_git\111.md</parameter></tool_c></tool_calls>`
	calls := ParseStandaloneToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected tool name Read, got %q", calls[0].Name)
	}
	if calls[0].Input["file_path"] != `D:\git_codes\google_adk_helloworld_git\111.md` {
		t.Fatalf("expected file_path argument, got %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsSupportsDirectBashTagWithProsePrefix(t *testing.T) {
	text := "我们有一个commit hash: c8192ae。需要看看这个commit具体改了什么。\n\n<bash> git show c8192ae9036540d454f0876ebe2860834695966d --stat </bash>"
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "git show c8192ae9036540d454f0876ebe2860834695966d --stat" {
		t.Fatalf("unexpected command: %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsSupportsDirectReadTag(t *testing.T) {
	text := `<read>D:\git_repos\ds2api\README.MD</read>`
	calls := ParseStandaloneToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected tool name Read, got %q", calls[0].Name)
	}
	if calls[0].Input["file_path"] != `D:\git_repos\ds2api\README.MD` {
		t.Fatalf("unexpected file_path: %#v", calls[0].Input)
	}
}

func TestParseStandaloneToolCallsDoesNotTreatUnknownDirectTagAsTool(t *testing.T) {
	text := `<div>hello</div>`
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 0 {
		t.Fatalf("expected unknown tag not parsed as tool call, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsSupportsUserProvidedToolCallMarkupSample(t *testing.T) {
	text := `<tool_calls>
<tool_call name="Bash">
<parameter name="command" string="true">cd /d/git_codes/google_adk_helloworld_git && python -c "open('_gen_prime.py','w',encoding='utf-8').write('import sys\nsys.stdout.reconfigure(encoding="utf-8")\ncode = chr(10).join(["def is_prime(n):"," if n<2: return False"," for i in range(2,int(n**0.5)+1):"," if n%i==0: return False"," return True","","primes=[n for n in range(1,101) if is_prime(n)]","print(f\"1~100以内的质数共{len(primes)}个:\")","for i,p in enumerate(primes,1): print(f\"{i:2d}. {p:3d}\")"])\nopen("prime_100.py","w",encoding="utf-8").write(code)\nprint("OK")\n')"</parameter>
</tool_call>
</tool_calls>`
	calls := ParseStandaloneToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected tool name Bash, got %q", calls[0].Name)
	}
	cmd, _ := calls[0].Input["command"].(string)
	if cmd == "" {
		t.Fatalf("expected non-empty command argument, got %#v", calls[0].Input)
	}
	if !strings.Contains(cmd, "python -c") {
		t.Fatalf("expected command to contain python -c, got %q", cmd)
	}
}

func TestParseStandaloneToolCallsIgnoresEmptyNestedToolCallsTags(t *testing.T) {
	text := `<tool_calls><tool_calls><tool_calls></tool_calls></tool_calls></tool_calls>`
	res := ParseStandaloneToolCallsDetailed(text, []string{"mcp__exa__web_search_exa"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected saw tool-call syntax, got %#v", res)
	}
	if len(res.Calls) != 0 {
		t.Fatalf("expected no parsed calls for empty nested tags, got %#v", res.Calls)
	}
}

func TestParseToolCallsSupportsAntmlFunctionAttributeWithParametersTag(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="x" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected canonical tool name bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsMultipleAntmlFunctionCalls(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="1" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call><antml:function_call id="2" function="Read"><antml:parameters>{"file_path":"README.md"}</antml:parameters></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash", "read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	if calls[0].Name != "bash" || calls[1].Name != "read" {
		t.Fatalf("expected canonical names [bash read], got %#v", calls)
	}
}

func TestParseToolCallsDoesNotAcceptMismatchedMarkupTags(t *testing.T) {
	text := `<tool_call><name>read_file</function><arguments>{"path":"README.md"}</arguments></tool_call>`
	calls := ParseToolCalls(text, []string{"read_file"})
	if len(calls) != 0 {
		t.Fatalf("expected mismatched tags to be rejected, got %#v", calls)
	}
}

func TestRepairInvalidJSONBackslashes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"path": "C:\Users\name"}`, `{"path": "C:\\Users\name"}`},
		{`{"cmd": "cd D:\git_codes"}`, `{"cmd": "cd D:\\git_codes"}`},
		{`{"text": "line1\nline2"}`, `{"text": "line1\nline2"}`},
		{`{"path": "D:\\back\\slash"}`, `{"path": "D:\\back\\slash"}`},
		{`{"unicode": "\u2705"}`, `{"unicode": "\u2705"}`},
		{`{"invalid_u": "\u123"}`, `{"invalid_u": "\\u123"}`},
	}

	for _, tt := range tests {
		got := repairInvalidJSONBackslashes(tt.input)
		if got != tt.expected {
			t.Errorf("repairInvalidJSONBackslashes(%s) = %s; want %s", tt.input, got, tt.expected)
		}
	}
}

func TestRepairLooseJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{tool_calls: [{"name": "search", "input": {"q": "go"}}]}`, `{"tool_calls": [{"name": "search", "input": {"q": "go"}}]}`},
		{`{name: "search", input: {q: "go"}}`, `{"name": "search", "input": {"q": "go"}}`},
	}

	for _, tt := range tests {
		got := RepairLooseJSON(tt.input)
		if got != tt.expected {
			t.Errorf("RepairLooseJSON(%s) = %s; want %s", tt.input, got, tt.expected)
		}
	}
}

func TestParseToolCallsWithUnquotedKeys(t *testing.T) {
	text := `这里是列表：{tool_calls: [{"name": "todowrite", "input": {"todos": "test"}}]}`
	availableTools := []string{"todowrite"}

	parsed := ParseToolCalls(text, availableTools)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed))
	}
	if parsed[0].Name != "todowrite" {
		t.Errorf("expected tool todowrite, got %s", parsed[0].Name)
	}
}

func TestParseToolCallsWithInvalidBackslashes(t *testing.T) {
	// DeepSeek sometimes outputs Windows paths with single backslashes in JSON strings
	// Note: using raw string to simulate what AI actually sends in the stream
	text := `好的，执行以下命令：{"name": "execute_command", "input": "{\"command\": \"cd D:\git_codes && dir\"}"}`
	availableTools := []string{"execute_command"}

	parsed := ParseToolCalls(text, availableTools)
	// If standard JSON fails, buildToolCallCandidates should still extract the object,
	// and parseToolCallsPayload should repair it.
	if len(parsed) != 1 {
		// If it still fails, let's see why
		candidates := buildToolCallCandidates(text)
		t.Logf("Candidates: %v", candidates)
		t.Fatalf("expected 1 tool call, got %d", len(parsed))
	}

	cmd, ok := parsed[0].Input["command"].(string)
	if !ok {
		t.Fatalf("expected command string in input, got %v", parsed[0].Input)
	}

	expected := "cd D:\\git_codes && dir"
	if cmd != expected {
		t.Errorf("expected command %q, got %q", expected, cmd)
	}
}

func TestParseToolCallsWithDeepSeekHallucination(t *testing.T) {
	// 模拟 DeepSeek 典型的幻觉输出：未加引号的键名 + 包含 Windows 路径的嵌套 JSON 字符串 + 漏掉列表的方括号
	text := `检测到实施意图——实现经典算法。需在misc/目录创建Python文件。
关键约束:
1. Windows UTF-8编码处理
2. 必须用绝对路径导入
3. 禁止write覆盖已有文件（misc/目录允许创建新文件）
将任务分解并委托：
- 研究8皇后算法模式（并行探索）
- 实现带可视化输出的解决方案（unspecified-high）
先创建todo列表追踪步骤。
{tool_calls: [{"name": "todowrite", "input": {"todos": {"content": "研究8皇后问题算法模式（回溯法）和输出格式", "status": "pending", "priority": "high"}, {"content": "在misc/目录创建8皇后Python脚本，包含完整解决方案和可视化输出", "status": "pending", "priority": "high"}, {"content": "验证脚本正确性（运行测试）", "status": "pending", "priority": "medium"}}}]}`

	availableTools := []string{"todowrite"}
	parsed := ParseToolCalls(text, availableTools)

	if len(parsed) != 1 {
		cands := buildToolCallCandidates(text)
		for i, c := range cands {
			t.Logf("CAND %d: %s", i, c)
			repaired := RepairLooseJSON(c)
			t.Logf("  REPAIRED: %s", repaired)
		}
		t.Fatalf("expected 1 tool call, got %d. Candidates: %v", len(parsed), buildToolCallCandidates(text))
	}

	if parsed[0].Name != "todowrite" {
		t.Errorf("expected tool name 'todowrite', got %q", parsed[0].Name)
	}

	todos, ok := parsed[0].Input["todos"].([]any)
	if !ok {
		t.Fatalf("expected 'todos' to be parsed as a list, got %T: %#v", parsed[0].Input["todos"], parsed[0].Input["todos"])
	}
	if len(todos) != 3 {
		t.Errorf("expected 3 todo items, got %d", len(todos))
	}
}

func TestParseToolCallsWithMixedWindowsPaths(t *testing.T) {
	// 更复杂的案例：嵌套 JSON 字符串中的反斜杠未转义
	text := `关键约束: 1. Windows UTF-8编码处理 2. 必须用绝对路径导入 D:\git_codes\ds2api\misc
{tool_calls: [{"name": "write_file", "input": "{\"path\": \"D:\\git_codes\\ds2api\\misc\\queens.py\", \"content\": \"print('hello')\"}"}]}`

	availableTools := []string{"write_file"}
	parsed := ParseToolCalls(text, availableTools)

	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool call from mixed text with paths, got %d", len(parsed))
	}

	path, _ := parsed[0].Input["path"].(string)
	// 在解析后的 Go map 中，反斜杠应该被还原
	if !strings.Contains(path, "D:\\git_codes") && !strings.Contains(path, "D:/git_codes") {
		t.Errorf("expected path to contain Windows style separators, got %q", path)
	}
}

func TestParseToolCallsDirectParenShellRejectsDanglingCDATA(t *testing.T) {
	text := `Bash(<![CDATA[git show --stat HEAD])`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 0 {
		t.Fatalf("expected dangling CDATA direct-paren shell call to be rejected, got %#v", calls)
	}
}

func TestParseToolCallsDirectParenShellUnwrapsCompleteCDATA(t *testing.T) {
	text := `Bash(<![CDATA[git show --stat HEAD]]>)`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected complete CDATA direct-paren shell call to be parsed, got %#v", calls)
	}
	if calls[0].Input["command"] != "git show --stat HEAD" {
		t.Fatalf("unexpected command after CDATA unwrap: %#v", calls[0].Input)
	}
}

func TestRepairLooseJSONWithNestedObjects(t *testing.T) {
	// 测试嵌套对象的修复：DeepSeek 幻觉输出，每个元素内部包含嵌套 {}
	// 注意：正则只支持单层嵌套，不支持更深层次的嵌套
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 1. 单层嵌套对象（核心修复目标）
		{
			name:     "单层嵌套 - 2个元素",
			input:    `"todos": {"content": "研究算法", "input": {"q": "8 queens"}}, {"content": "实现", "input": {"path": "queens.py"}}`,
			expected: `"todos": [{"content": "研究算法", "input": {"q": "8 queens"}}, {"content": "实现", "input": {"path": "queens.py"}}]`,
		},
		// 2. 3个单层嵌套对象
		{
			name:     "3个单层嵌套对象",
			input:    `"items": {"a": {"x":1}}, {"b": {"y":2}}, {"c": {"z":3}}`,
			expected: `"items": [{"a": {"x":1}}, {"b": {"y":2}}, {"c": {"z":3}}]`,
		},
		// 3. 混合嵌套：有些字段是对象，有些是原始值
		{
			name:     "混合嵌套 - 对象和原始值混合",
			input:    `"items": {"name": "test", "config": {"timeout": 30}}, {"name": "test2", "config": {"timeout": 60}}`,
			expected: `"items": [{"name": "test", "config": {"timeout": 30}}, {"name": "test2", "config": {"timeout": 60}}]`,
		},
		// 4. 4个嵌套对象（边界测试）
		{
			name:     "4个嵌套对象",
			input:    `"todos": {"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}`,
			expected: `"todos": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}]`,
		},
		// 5. DeepSeek 典型幻觉：无空格逗号分隔
		{
			name:     "无空格逗号分隔",
			input:    `"results": {"name": "a"}, {"name": "b"}, {"name": "c"}`,
			expected: `"results": [{"name": "a"}, {"name": "b"}, {"name": "c"}]`,
		},
		// 6. 嵌套数组（数组在对象内，不是深层嵌套）
		{
			name:     "对象内包含数组",
			input:    `"data": {"items": [1,2,3]}, {"items": [4,5,6]}`,
			expected: `"data": [{"items": [1,2,3]}, {"items": [4,5,6]}]`,
		},
		// 7. 真实的 DeepSeek 8皇后问题输出
		{
			name:     "DeepSeek 8皇后真实输出",
			input:    `"todos": {"content": "研究8皇后算法", "status": "pending"}, {"content": "实现Python脚本", "status": "pending"}, {"content": "验证结果", "status": "pending"}`,
			expected: `"todos": [{"content": "研究8皇后算法", "status": "pending"}, {"content": "实现Python脚本", "status": "pending"}, {"content": "验证结果", "status": "pending"}]`,
		},
		// 8. 简单无嵌套对象（回归测试）
		{
			name:     "简单无嵌套对象",
			input:    `"items": {"a": 1}, {"b": 2}`,
			expected: `"items": [{"a": 1}, {"b": 2}]`,
		},
		// 9. 更复杂的单层嵌套
		{
			name:     "复杂单层嵌套",
			input:    `"functions": {"name": "execute", "input": {"command": "ls"}}, {"name": "read", "input": {"file": "a.txt"}}`,
			expected: `"functions": [{"name": "execute", "input": {"command": "ls"}}, {"name": "read", "input": {"file": "a.txt"}}]`,
		},
		// 10. 5个嵌套对象
		{
			name:     "5个嵌套对象",
			input:    `"tasks": {"id":1}, {"id":2}, {"id":3}, {"id":4}, {"id":5}`,
			expected: `"tasks": [{"id":1}, {"id":2}, {"id":3}, {"id":4}, {"id":5}]`,
		},
	}

	for _, tt := range tests {
		got := RepairLooseJSON(tt.input)
		if got != tt.expected {
			t.Errorf("[%s] RepairLooseJSON with nested objects:\n  input:    %s\n  got:      %s\n  expected: %s", tt.name, tt.input, got, tt.expected)
		}
	}
}
