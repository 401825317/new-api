package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

const (
	modelPricingSyncTaskDefaultIntervalHours = 24
	modelPricingSyncTaskDefaultTimeout       = 20 * time.Second
	modelPricingSyncTaskMaxResponseBytes     = 10 << 20 // 10MB
	modelPricingSyncTaskDefaultSources       = "official,models_dev"
)

var (
	modelPricingSyncTaskOnce    sync.Once
	modelPricingSyncTaskRunning atomic.Bool
)

type modelPricingSyncSource struct {
	Name string
	URL  string
}

type modelPricingSyncSourceResult struct {
	Source modelPricingSyncSource
	Data   map[string]any
}

func pricingSyncOptionKey(field string) string {
	switch field {
	case "model_ratio":
		return "ModelRatio"
	case "completion_ratio":
		return "CompletionRatio"
	case "cache_ratio":
		return "CacheRatio"
	case "create_cache_ratio":
		return "CreateCacheRatio"
	case "image_ratio":
		return "ImageRatio"
	case "audio_ratio":
		return "AudioRatio"
	case "audio_completion_ratio":
		return "AudioCompletionRatio"
	case "model_price":
		return "ModelPrice"
	case billing_setting.BillingModeField:
		return "billing_setting.billing_mode"
	case billing_setting.BillingExprField:
		return "billing_setting.billing_expr"
	default:
		return field
	}
}

func modelPricingSyncSourcesFromEnv() []modelPricingSyncSource {
	rawSources := common.GetEnvOrDefaultString("MODEL_PRICING_SYNC_TASK_SOURCES", modelPricingSyncTaskDefaultSources)
	parts := strings.Split(rawSources, ",")
	sources := make([]modelPricingSyncSource, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		switch strings.ToLower(token) {
		case "official", "basellm":
			sources = append(sources, modelPricingSyncSource{
				Name: "official",
				URL:  officialRatioPresetBaseURL + "/llm-metadata/api/newapi/ratio_config-v1-base.json",
			})
		case "models_dev", "models.dev":
			sources = append(sources, modelPricingSyncSource{
				Name: "models.dev",
				URL:  modelsDevPresetBaseURL + modelsDevPath,
			})
		default:
			if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
				sources = append(sources, modelPricingSyncSource{
					Name: token,
					URL:  token,
				})
			}
		}
	}
	return sources
}

func fetchModelPricingSyncSource(ctx context.Context, client *http.Client, source modelPricingSyncSource) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "new-api-pricing-sync/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, modelPricingSyncTaskMaxResponseBytes))
	if err != nil {
		return nil, err
	}

	if isModelsDevAPIEndpoint(source.URL) {
		return convertModelsDevToRatioData(bytes.NewReader(body))
	}
	return parseRatioConfigResponse(body)
}

func parseRatioConfigResponse(body []byte) (map[string]any, error) {
	payload := body

	var wrapped struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := common.Unmarshal(body, &wrapped); err == nil && len(wrapped.Data) > 0 {
		if !wrapped.Success && strings.TrimSpace(wrapped.Message) != "" {
			return nil, fmt.Errorf("%s", wrapped.Message)
		}
		payload = wrapped.Data
	}

	var data map[string]any
	if err := common.Unmarshal(payload, &data); err != nil {
		return nil, err
	}

	for _, field := range pricingSyncFields {
		if _, ok := data[field]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("response does not contain pricing sync fields")
}

func buildMissingPricingOptionUpdates(localData map[string]any, sources []modelPricingSyncSourceResult) (map[string]string, int, error) {
	updates := make(map[string]string)
	added := 0

	for _, field := range pricingSyncFields {
		currentMap := valueMap(localData[field])
		merged := make(map[string]any, len(currentMap))
		for modelName, value := range currentMap {
			merged[modelName] = normalizeSyncValue(field, value)
		}

		fieldAdded := 0
		for _, source := range sources {
			sourceMap := valueMap(source.Data[field])
			for modelName, value := range sourceMap {
				modelName = strings.TrimSpace(modelName)
				if modelName == "" {
					continue
				}
				if _, exists := merged[modelName]; exists {
					continue
				}
				merged[modelName] = normalizeSyncValue(field, value)
				fieldAdded++
			}
		}

		if fieldAdded == 0 {
			continue
		}
		optionValue, err := common.Marshal(merged)
		if err != nil {
			return nil, 0, err
		}
		updates[pricingSyncOptionKey(field)] = string(optionValue)
		added += fieldAdded
	}

	return updates, added, nil
}

func runModelPricingSyncTaskOnce() {
	if !modelPricingSyncTaskRunning.CompareAndSwap(false, true) {
		return
	}
	defer modelPricingSyncTaskRunning.Store(false)

	sources := modelPricingSyncSourcesFromEnv()
	if len(sources) == 0 {
		common.SysLog("model pricing sync task skipped: no valid sources")
		return
	}

	client := &http.Client{Timeout: modelPricingSyncTaskDefaultTimeout}
	results := make([]modelPricingSyncSourceResult, 0, len(sources))
	for _, source := range sources {
		ctx, cancel := context.WithTimeout(context.Background(), modelPricingSyncTaskDefaultTimeout)
		data, err := fetchModelPricingSyncSource(ctx, client, source)
		cancel()
		if err != nil {
			common.SysLog(fmt.Sprintf("model pricing sync source failed: source=%s err=%v", source.Name, err))
			continue
		}
		results = append(results, modelPricingSyncSourceResult{Source: source, Data: data})
	}

	if len(results) == 0 {
		common.SysLog("model pricing sync task skipped: all sources failed")
		return
	}

	updates, added, err := buildMissingPricingOptionUpdates(getLocalPricingSyncData(), results)
	if err != nil {
		common.SysLog(fmt.Sprintf("model pricing sync task build updates failed: %v", err))
		return
	}
	if len(updates) == 0 || added == 0 {
		common.SysLog(fmt.Sprintf("model pricing sync task done: sources=%d added=0", len(results)))
		return
	}

	if err := model.UpdateOptionsBulk(updates); err != nil {
		common.SysLog(fmt.Sprintf("model pricing sync task update options failed: %v", err))
		return
	}
	common.SysLog(fmt.Sprintf("model pricing sync task done: sources=%d updated_options=%d added_entries=%d", len(results), len(updates), added))
}

func StartModelPricingSyncTask() {
	modelPricingSyncTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		if !common.GetEnvOrDefaultBool("MODEL_PRICING_SYNC_TASK_ENABLED", false) {
			return
		}

		intervalHours := common.GetEnvOrDefault("MODEL_PRICING_SYNC_TASK_INTERVAL_HOURS", modelPricingSyncTaskDefaultIntervalHours)
		if intervalHours < 1 {
			intervalHours = modelPricingSyncTaskDefaultIntervalHours
		}
		interval := time.Duration(intervalHours) * time.Hour
		runOnStart := common.GetEnvOrDefaultBool("MODEL_PRICING_SYNC_TASK_RUN_ON_START", true)

		go func() {
			common.SysLog(fmt.Sprintf("model pricing sync task started: interval=%s sources=%s", interval, common.GetEnvOrDefaultString("MODEL_PRICING_SYNC_TASK_SOURCES", modelPricingSyncTaskDefaultSources)))
			if runOnStart {
				runModelPricingSyncTaskOnce()
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				runModelPricingSyncTaskOnce()
			}
		}()
	})
}
