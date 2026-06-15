package controller

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	modelMetadataSyncTaskDefaultIntervalHours = 24
	modelMetadataSyncTaskDefaultLocale        = "zh"
)

var (
	modelMetadataSyncTaskOnce    sync.Once
	modelMetadataSyncTaskRunning atomic.Bool
)

func runModelMetadataSyncTaskOnce() {
	if !modelMetadataSyncTaskRunning.CompareAndSwap(false, true) {
		return
	}
	defer modelMetadataSyncTaskRunning.Store(false)

	locale := common.GetEnvOrDefaultString("MODEL_METADATA_SYNC_TASK_LOCALE", modelMetadataSyncTaskDefaultLocale)
	result, err := syncUpstreamModelMetadata(context.Background(), syncRequest{Locale: locale})
	if err != nil {
		common.SysLog(fmt.Sprintf("model metadata sync task failed: %v", err))
		return
	}
	common.SysLog(fmt.Sprintf(
		"model metadata sync task done: locale=%s created_models=%d created_vendors=%d skipped_models=%d source=%s",
		result.Source.Locale,
		result.CreatedModels,
		result.CreatedVendors,
		len(result.SkippedModels),
		result.Source.ModelsURL,
	))
}

func StartModelMetadataSyncTask() {
	modelMetadataSyncTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		if !common.GetEnvOrDefaultBool("MODEL_METADATA_SYNC_TASK_ENABLED", false) {
			return
		}

		intervalHours := common.GetEnvOrDefault("MODEL_METADATA_SYNC_TASK_INTERVAL_HOURS", modelMetadataSyncTaskDefaultIntervalHours)
		if intervalHours < 1 {
			intervalHours = modelMetadataSyncTaskDefaultIntervalHours
		}
		interval := time.Duration(intervalHours) * time.Hour
		runOnStart := common.GetEnvOrDefaultBool("MODEL_METADATA_SYNC_TASK_RUN_ON_START", true)

		go func() {
			common.SysLog(fmt.Sprintf("model metadata sync task started: interval=%s locale=%s", interval, common.GetEnvOrDefaultString("MODEL_METADATA_SYNC_TASK_LOCALE", modelMetadataSyncTaskDefaultLocale)))
			if runOnStart {
				runModelMetadataSyncTaskOnce()
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				runModelMetadataSyncTaskOnce()
			}
		}()
	})
}
