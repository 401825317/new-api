package helper

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/dto"
)

// StripForeignResponsesState removes opaque provider-bound Responses items
// before replaying a request through another OpenAI-compatible provider.
// Visible text, tool calls, and tool outputs remain intact.
func StripForeignResponsesState(request *dto.OpenAIResponsesRequest) (removedReasoning int, removedCompaction int, err error) {
	if request == nil || len(request.Input) == 0 {
		return 0, 0, nil
	}

	var input []json.RawMessage
	if err := json.Unmarshal(request.Input, &input); err != nil {
		// Responses also accepts a plain input string; there is no state to strip.
		return 0, 0, nil
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
		if hasEncryptedContent(item) && isProviderBoundResponsesState(itemType) {
			if itemType == "reasoning" {
				removedReasoning++
			} else {
				removedCompaction++
			}
			continue
		}

		// Some clients nest provider-bound reasoning inside message.content.
		var content []json.RawMessage
		if err := json.Unmarshal(item["content"], &content); err == nil {
			cleaned := make([]json.RawMessage, 0, len(content))
			changed := false
			for _, rawContent := range content {
				var contentItem map[string]json.RawMessage
				if err := json.Unmarshal(rawContent, &contentItem); err != nil {
					cleaned = append(cleaned, rawContent)
					continue
				}
				var contentType string
				_ = json.Unmarshal(contentItem["type"], &contentType)
				if hasEncryptedContent(contentItem) && isProviderBoundResponsesState(contentType) {
					if contentType == "reasoning" {
						removedReasoning++
					} else {
						removedCompaction++
					}
					changed = true
					continue
				}
				cleaned = append(cleaned, rawContent)
			}
			if changed {
				if len(cleaned) == 0 && itemType == "reasoning" {
					continue
				}
				item["content"], err = json.Marshal(cleaned)
				if err != nil {
					return 0, 0, fmt.Errorf("marshal Responses fallback content: %w", err)
				}
				rawItem, err = json.Marshal(item)
				if err != nil {
					return 0, 0, fmt.Errorf("marshal Responses fallback item: %w", err)
				}
			}
		}
		filtered = append(filtered, rawItem)
	}

	if removedReasoning+removedCompaction == 0 {
		return 0, 0, nil
	}
	request.Input, err = json.Marshal(filtered)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal Responses fallback input: %w", err)
	}
	return removedReasoning, removedCompaction, nil
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
