package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// NormalizeInputForUpstream removes response-only fields from replayed Responses
// items and normalizes the legacy system role to the Responses API developer role.
// It intentionally changes only top-level input items: a status value nested in a
// tool result is user data and must remain untouched.
func (r *OpenAIResponsesRequest) NormalizeInputForUpstream() error {
	if len(r.Input) == 0 {
		return nil
	}

	normalized, changed, err := NormalizeResponsesInput(r.Input)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	r.Input = normalized
	return nil
}

// NormalizeResponsesRequestBody applies the same compatibility rule to a raw
// request body while preserving fields unknown to this DTO. It is used when a
// channel has pass-through enabled and the normal DTO conversion is bypassed.
func NormalizeResponsesRequestBody(body []byte) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, false, err
	}
	input, ok := fields["input"]
	if !ok {
		return body, false, nil
	}

	normalized, changed, err := NormalizeResponsesInput(input)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return body, false, nil
	}
	fields["input"] = normalized
	result, err := common.Marshal(fields)
	if err != nil {
		return nil, false, err
	}
	return result, true, nil
}

// NormalizeResponsesInput removes response-only fields from top-level replay
// items and maps the legacy system role to developer. Nested tool-result JSON
// is left untouched because its status fields are application data.
func NormalizeResponsesInput(input json.RawMessage) (json.RawMessage, bool, error) {
	var value any
	if err := common.Unmarshal(input, &value); err != nil {
		return nil, false, err
	}

	changed := false
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if normalizeResponsesInputItem(item) {
				changed = true
			}
		}
	case map[string]any:
		changed = normalizeResponsesInputItem(value)
	}
	if !changed {
		return input, false, nil
	}

	normalized, err := common.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(normalized), true, nil
}

func normalizeResponsesInputItem(item any) bool {
	record, ok := item.(map[string]any)
	if !ok {
		return false
	}

	changed := false
	if _, exists := record["status"]; exists {
		delete(record, "status")
		changed = true
	}
	if role, ok := record["role"].(string); ok && role == "system" {
		record["role"] = "developer"
		changed = true
	}
	return changed
}
