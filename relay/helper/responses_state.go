package helper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ResponsesStatePolicy 描述“本次 attempt 实际要打到的上游，能不能校验客户端回放过来的
// provider-bound Responses 状态”。不同上游家族对同一段回放历史的可读性不同，所以清洗策略
// 必须跟着目标上游走，不能按客户端请求一刀切。
type ResponsesStatePolicy struct {
	// PassThroughReasoning 为 true 时，reasoning item 原样回传，网关不做任何改写。
	//
	// 触发条件：本次 attempt 的目标上游属于 DeepSeek 系（原生渠道，或 OpenAI 兼容渠道
	// 映射到 deepseek-* 模型）。这类上游在 thinking 模式下要求客户端把上一轮的推理状态
	// 一起回传，删掉任意一部分都可能被直接拒绝：
	// "The reasoning_text in the thinking mode must be passed back to the API"
	//
	// 关键事实：Codex 回放历史时只带 encrypted_content（本地 rollout 记录里 reasoning
	// item 恒为 content 为空、encrypted_content 非空），而这段 blob 本来就是该 DeepSeek
	// 上游自己签发的、只有它能解密。按“看起来不透明就丢掉”的通用清洗规则处理，等于把
	// 上游唯一能读的连续性证据删掉，反而制造出上面那条 400。
	//
	// 失败边界：这是“同族回放”策略，前提是回放状态与目标上游同源。跨族兜底（例如历史
	// 状态来自 OpenAI 系、本次因重试落到 DeepSeek）时，blob 无法被目标上游解密，是否报错
	// 取决于上游实现；此时宁可让上游显式拒绝，也不删掉同源场景唯一可用的状态。
	PassThroughReasoning bool
}

// ResponsesStateStripResult 汇总本次清洗结果，供调用方记录日志并判断是否需要同步降级
// （例如无法回放推理状态时关闭 thinking）。
type ResponsesStateStripResult struct {
	// RemovedReasoning 被整条删除的 reasoning item 数量。
	RemovedReasoning int
	// RemovedCompaction 被整条删除的 compaction / compaction_summary item 数量。
	RemovedCompaction int
	// PassedThroughReasoning 按同族策略原样回传、未被改写的 reasoning item 数量。
	// 它代表“状态保真”的项，不计入 Removed。
	PassedThroughReasoning int
}

// Removed 返回被整条删除的 provider-bound item 总数。
func (r ResponsesStateStripResult) Removed() int {
	return r.RemovedReasoning + r.RemovedCompaction
}

// Changed 表示本次 attempt 的 payload 与客户端原始回放是否存在差异（删除或保真回传），
// 调用方用它决定是否记录诊断日志。
func (r ResponsesStateStripResult) Changed() bool {
	return r.Removed() > 0 || r.PassedThroughReasoning > 0
}

// ResponsesStatePolicyForUpstream 根据本次 attempt 实际要打到的上游推导回放策略。
//
// 判定顺序：渠道类型优先（DeepSeek 原生渠道一定按 DeepSeek 语义处理），其次回退到模型名
// 判定，用于 OpenAI 兼容渠道承载 DeepSeek 模型（例如 type=1 渠道映射 deepseek-v4.x）
// 这一类生产上最常见的组合。
// 无法识别上游家族时返回零值策略，即维持“整条丢弃不透明状态”的保守行为。
func ResponsesStatePolicyForUpstream(info *relaycommon.RelayInfo) ResponsesStatePolicy {
	if info == nil {
		return ResponsesStatePolicy{}
	}
	if info.ChannelMeta != nil {
		if info.ChannelMeta.ChannelType == constant.ChannelTypeDeepSeek {
			return ResponsesStatePolicy{PassThroughReasoning: true}
		}
		if strings.Contains(strings.ToLower(info.ChannelMeta.UpstreamModelName), "deepseek") {
			return ResponsesStatePolicy{PassThroughReasoning: true}
		}
	}
	if strings.Contains(strings.ToLower(info.OriginModelName), "deepseek") {
		return ResponsesStatePolicy{PassThroughReasoning: true}
	}
	return ResponsesStatePolicy{}
}

// StripForeignResponsesState neutralises provider-bound Responses items before
// replaying a request through another OpenAI-compatible provider. Visible text,
// tool calls, and tool outputs always remain intact; reasoning state is either
// replayed verbatim (DeepSeek family) or dropped (everyone else).
//
// 保留策略由 policy 决定：DeepSeek 系上游（见 ResponsesStatePolicy.PassThroughReasoning）
// 的 reasoning item 原样回传；其余上游仍然整条丢弃无法校验的状态。compaction /
// compaction_summary 属于纯 provider 侧状态，没有可读等价物，任何上游下都整条丢弃。
func StripForeignResponsesState(request *dto.OpenAIResponsesRequest, policy ResponsesStatePolicy) (ResponsesStateStripResult, error) {
	var result ResponsesStateStripResult
	if request == nil || len(request.Input) == 0 {
		return result, nil
	}

	var input []json.RawMessage
	if err := json.Unmarshal(request.Input, &input); err != nil {
		// Responses also accepts a plain input string; there is no state to strip.
		return result, nil
	}

	filtered := make([]json.RawMessage, 0, len(input))
	for _, rawItem := range input {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			filtered = append(filtered, rawItem)
			continue
		}

		var itemType string
		_ = json.Unmarshal(item["type"], &itemType)

		changed := false

		// 顶层 provider-bound 状态：reasoning / compaction / compaction_summary。
		if hasEncryptedContent(item) && isProviderBoundResponsesState(itemType) {
			keep, stripped := sanitizeProviderBoundResponsesItem(item, itemType, policy, &result)
			if !keep {
				continue
			}
			changed = changed || stripped
		}

		// Some clients nest provider-bound reasoning inside message.content.
		keep, contentChanged, err := sanitizeNestedResponsesState(item, itemType, policy, &result)
		if err != nil {
			return ResponsesStateStripResult{}, err
		}
		if !keep {
			continue
		}
		changed = changed || contentChanged

		if !changed {
			filtered = append(filtered, rawItem)
			continue
		}
		rewritten, err := marshalResponsesItem(item)
		if err != nil {
			return ResponsesStateStripResult{}, err
		}
		filtered = append(filtered, rewritten)
	}

	if result.Removed() == 0 {
		// 没有任何状态被删除，保持原始 Input 字节不变（原样回传路径不重新序列化）。
		return result, nil
	}
	updated, err := json.Marshal(filtered)
	if err != nil {
		return ResponsesStateStripResult{}, fmt.Errorf("marshal Responses fallback input: %w", err)
	}
	request.Input = updated
	return result, nil
}

// sanitizeProviderBoundResponsesItem 对单个 reasoning / compaction 类 item 应用 policy。
//
// 返回 keep 表示是否保留回放，stripped 表示是否就地改写了该 item（调用方必须重新序列化）。
// 同族回放分支返回 keep=true、stripped=false，即原样回传、不改写任何字段。
func sanitizeProviderBoundResponsesItem(item map[string]json.RawMessage, itemType string, policy ResponsesStatePolicy, result *ResponsesStateStripResult) (keep bool, stripped bool) {
	if itemType == "reasoning" {
		if policy.PassThroughReasoning {
			// 同族回放：连 encrypted_content 一起原样送回，网关不改写任何字段。
			result.PassedThroughReasoning++
			return true, false
		}
		result.RemovedReasoning++
		return false, false
	}

	// compaction / compaction_summary 只有原 provider 自己能解读，且没有可读文本可回放，
	// 因此在任何上游下都整条丢弃。
	result.RemovedCompaction++
	return false, false
}

// sanitizeNestedResponsesState 清洗 message.content 里内嵌的 provider-bound 状态。
// 返回 keep=false 表示该外层 item 已没有可用内容，需要整条丢弃。
func sanitizeNestedResponsesState(item map[string]json.RawMessage, itemType string, policy ResponsesStatePolicy, result *ResponsesStateStripResult) (keep bool, changed bool, err error) {
	rawContent, ok := item["content"]
	if !ok {
		return true, false, nil
	}

	var content []json.RawMessage
	if err := json.Unmarshal(rawContent, &content); err != nil {
		return true, false, nil
	}

	cleaned := make([]json.RawMessage, 0, len(content))
	for _, rawEntry := range content {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			cleaned = append(cleaned, rawEntry)
			continue
		}

		var entryType string
		_ = json.Unmarshal(entry["type"], &entryType)
		if !hasEncryptedContent(entry) || !isProviderBoundResponsesState(entryType) {
			cleaned = append(cleaned, rawEntry)
			continue
		}

		entryKeep, entryStripped := sanitizeProviderBoundResponsesItem(entry, entryType, policy, result)
		if !entryKeep {
			changed = true
			continue
		}
		if !entryStripped {
			cleaned = append(cleaned, rawEntry)
			continue
		}
		rewritten, err := marshalResponsesItem(entry)
		if err != nil {
			return false, false, err
		}
		cleaned = append(cleaned, rewritten)
		changed = true
	}

	if !changed {
		return true, false, nil
	}
	if len(cleaned) == 0 && itemType == "reasoning" {
		// 外层 reasoning item 的内容被清空后已无可回放信息，整条丢弃。
		return false, false, nil
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return false, false, fmt.Errorf("marshal Responses fallback content: %w", err)
	}
	item["content"] = encoded
	return true, true, nil
}

func marshalResponsesItem(item map[string]json.RawMessage) (json.RawMessage, error) {
	rewritten, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("marshal Responses fallback item: %w", err)
	}
	return rewritten, nil
}

func isProviderBoundResponsesState(itemType string) bool {
	return itemType == "reasoning" || itemType == "compaction" || itemType == "compaction_summary"
}

func hasEncryptedContent(item map[string]json.RawMessage) bool {
	var encryptedContent string
	if raw, ok := item["encrypted_content"]; ok {
		_ = json.Unmarshal(raw, &encryptedContent)
	}
	return encryptedContent != ""
}
