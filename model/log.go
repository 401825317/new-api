package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	uclawDesktopClientMarker     = "desktop"
	maxUClawVersionLength        = 64
	maxUClawCommitLength         = 64
	maxUClawBuildIDLength        = 128
	maxUClawRuntimeLabelLength   = 32
	maxUClawRequestIDLength      = 128
	maxUClawVersionUsageRows     = 200000
	uclawVersionCandidatePattern = `%"uclaw_version"%`
	legacyClawXCandidatePattern  = `%"clawx_version"%`
)

func sanitizeUClawDiagnosticValue(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLength {
		return ""
	}
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' ||
			strings.ContainsRune("._:+-", rune(char)) {
			continue
		}
		return ""
	}
	return value
}

func uclawDiagnosticHeader(c *gin.Context, name string, maxLength int) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return sanitizeUClawDiagnosticValue(c.GetHeader(name), maxLength)
}

func hasManagedUClawContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	// ClawXAuth only sets these values after validating an active managed
	// device session. Relay requests use the normal token middleware, so they
	// require an active device-to-token binding as an additional proof.
	if strings.TrimSpace(c.GetString("clawx_device_id")) != "" &&
		strings.TrimSpace(c.GetString("clawx_session_id")) != "" {
		return true
	}
	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")
	if userId <= 0 || tokenId <= 0 {
		return false
	}
	linked, err := IsActiveClawXDeviceToken(userId, tokenId)
	return err == nil && linked
}

func buildClientDiagnostics(c *gin.Context) map[string]interface{} {
	if c == nil || c.Request == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetHeader("X-UClaw-Client")), uclawDesktopClientMarker) {
		return nil
	}
	if !hasManagedUClawContext(c) {
		return nil
	}

	clientHeaderFields := []struct {
		key       string
		header    string
		maxLength int
	}{
		{key: "uclaw_version", header: "X-UClaw-Version", maxLength: maxUClawVersionLength},
		{key: "uclaw_commit", header: "X-UClaw-Commit", maxLength: maxUClawCommitLength},
		{key: "uclaw_build_id", header: "X-UClaw-Build-Id", maxLength: maxUClawBuildIDLength},
		{key: "uclaw_platform", header: "X-UClaw-Platform", maxLength: maxUClawRuntimeLabelLength},
		{key: "uclaw_arch", header: "X-UClaw-Arch", maxLength: maxUClawRuntimeLabelLength},
		{key: "uclaw_channel", header: "X-UClaw-Channel", maxLength: maxUClawRuntimeLabelLength},
		{key: "uclaw_mode", header: "X-UClaw-Mode", maxLength: maxUClawRuntimeLabelLength},
		{key: "uclaw_request_id", header: "X-Request-Id", maxLength: maxUClawRequestIDLength},
	}
	clientInfo := map[string]interface{}{}
	for _, field := range clientHeaderFields {
		if value := uclawDiagnosticHeader(c, field.header, field.maxLength); value != "" {
			clientInfo[field.key] = value
		}
	}
	if _, hasVersion := clientInfo["uclaw_version"]; !hasVersion {
		return nil
	}
	clientInfo["uclaw_client"] = uclawDesktopClientMarker
	return clientInfo
}

func recordMinimalUClawVersionSuccess(c *gin.Context, useTimeSeconds int) {
	diagnostics := buildClientDiagnostics(c)
	if len(diagnostics) == 0 {
		return
	}
	// The disabled detailed-log path records only anonymous release health.
	// Request ids are intentionally omitted because they are not needed for
	// aggregate success-rate or latency calculations.
	delete(diagnostics, "uclaw_request_id")
	log := &Log{
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeConsume,
		UseTime:   useTimeSeconds,
		Other: common.MapToJsonStr(map[string]interface{}{
			"client_diagnostics": diagnostics,
		}),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		logger.LogError(c, "failed to record minimal UClaw version metric: "+err.Error())
	}
}

func replaceClientDiagnostics(c *gin.Context, other map[string]interface{}) {
	if other == nil {
		return
	}
	delete(other, "client_diagnostics")
	if clientDiagnostics := buildClientDiagnostics(c); len(clientDiagnostics) > 0 {
		other["client_diagnostics"] = clientDiagnostics
	}
}

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		pattern, err := sanitizeLikePattern(value)
		if err != nil {
			return nil, err
		}
		return tx.Where(column+" LIKE ? ESCAPE '!'", pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// Remove operation-audit details (operator/route info), admin-only.
			delete(otherMap, "audit_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

type UClawVersionUsageFilter struct {
	StartTimestamp int64
	EndTimestamp   int64
}

type UClawVersionUsageStat struct {
	Version          string  `json:"version"`
	Commit           string  `json:"commit"`
	BuildId          string  `json:"build_id"`
	Platform         string  `json:"platform"`
	Arch             string  `json:"arch"`
	Channel          string  `json:"channel"`
	Mode             string  `json:"mode"`
	RequestCount     int64   `json:"request_count"`
	SuccessCount     int64   `json:"success_count"`
	ErrorCount       int64   `json:"error_count"`
	SuccessRate      float64 `json:"success_rate"`
	ErrorRate        float64 `json:"error_rate"`
	AverageLatencyMs float64 `json:"average_latency_ms"`
	P95LatencyMs     float64 `json:"p95_latency_ms"`
}

type UClawVersionUsageSummary struct {
	StartTimestamp int64                   `json:"start_timestamp"`
	EndTimestamp   int64                   `json:"end_timestamp"`
	TotalRequests  int64                   `json:"total_requests"`
	Truncated      bool                    `json:"truncated"`
	Items          []UClawVersionUsageStat `json:"items"`
}

type uclawVersionLogRow struct {
	Id      int
	Type    int
	UseTime int
	Other   string
}

type uclawVersionLogOther struct {
	EndToEndUpstreamResponseMs float64 `json:"end_to_end_upstream_response_ms"`
	UpstreamResponseMs         float64 `json:"upstream_response_ms"`
	ClientDiagnostics          struct {
		UClawClient   string `json:"uclaw_client"`
		UClawVersion  string `json:"uclaw_version"`
		UClawCommit   string `json:"uclaw_commit"`
		UClawBuildId  string `json:"uclaw_build_id"`
		UClawPlatform string `json:"uclaw_platform"`
		UClawArch     string `json:"uclaw_arch"`
		UClawChannel  string `json:"uclaw_channel"`
		UClawMode     string `json:"uclaw_mode"`
		ClawXClient   string `json:"clawx_client"`
		ClawXVersion  string `json:"clawx_version"`
		ClawXCommit   string `json:"clawx_commit"`
		ClawXBuildId  string `json:"clawx_build_id"`
		ClawXPlatform string `json:"clawx_platform"`
		ClawXArch     string `json:"clawx_arch"`
		ClawXChannel  string `json:"clawx_channel"`
		ClawXMode     string `json:"clawx_mode"`
	} `json:"client_diagnostics"`
}

type uclawVersionUsageAccumulator struct {
	stat       UClawVersionUsageStat
	latencies  []float64
	latencySum float64
}

func firstNonEmptyLogValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func uclawVersionLatencyMs(row uclawVersionLogRow, other uclawVersionLogOther) float64 {
	if other.EndToEndUpstreamResponseMs > 0 {
		return other.EndToEndUpstreamResponseMs
	}
	if other.UpstreamResponseMs > 0 {
		return other.UpstreamResponseMs
	}
	if row.UseTime > 0 {
		return float64(row.UseTime) * 1000
	}
	return 0
}

func percentile95(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	index := int(float64(len(values))*0.95+0.999999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func GetUClawVersionUsageSummary(filter UClawVersionUsageFilter) (UClawVersionUsageSummary, error) {
	summary := UClawVersionUsageSummary{
		StartTimestamp: filter.StartTimestamp,
		EndTimestamp:   filter.EndTimestamp,
		Items:          make([]UClawVersionUsageStat, 0),
	}
	query := LOG_DB.Model(&Log{}).
		Select("id, type, use_time, other").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("(other LIKE ? OR other LIKE ?)", uclawVersionCandidatePattern, legacyClawXCandidatePattern)
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	rows, err := query.Order("id desc").Limit(maxUClawVersionUsageRows + 1).Rows()
	if err != nil {
		return summary, err
	}
	defer rows.Close()

	groups := make(map[string]*uclawVersionUsageAccumulator)
	scannedRows := 0
	for rows.Next() {
		scannedRows++
		if scannedRows > maxUClawVersionUsageRows {
			summary.Truncated = true
			break
		}
		var row uclawVersionLogRow
		if err := LOG_DB.ScanRows(rows, &row); err != nil {
			return summary, err
		}
		var other uclawVersionLogOther
		if err := common.UnmarshalJsonStr(row.Other, &other); err != nil {
			continue
		}
		diagnostics := other.ClientDiagnostics
		client := firstNonEmptyLogValue(diagnostics.UClawClient, diagnostics.ClawXClient)
		if !strings.EqualFold(client, uclawDesktopClientMarker) && !strings.EqualFold(client, "uclaw") {
			continue
		}
		version := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawVersion, diagnostics.ClawXVersion), maxUClawVersionLength)
		if version == "" {
			continue
		}
		commit := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawCommit, diagnostics.ClawXCommit), maxUClawCommitLength)
		buildId := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawBuildId, diagnostics.ClawXBuildId), maxUClawBuildIDLength)
		platform := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawPlatform, diagnostics.ClawXPlatform), maxUClawRuntimeLabelLength)
		arch := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawArch, diagnostics.ClawXArch), maxUClawRuntimeLabelLength)
		channel := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawChannel, diagnostics.ClawXChannel), maxUClawRuntimeLabelLength)
		mode := sanitizeUClawDiagnosticValue(firstNonEmptyLogValue(diagnostics.UClawMode, diagnostics.ClawXMode), maxUClawRuntimeLabelLength)
		key := strings.Join([]string{version, commit, buildId, platform, arch, channel, mode}, "\x00")
		group := groups[key]
		if group == nil {
			group = &uclawVersionUsageAccumulator{
				stat: UClawVersionUsageStat{
					Version:  version,
					Commit:   commit,
					BuildId:  buildId,
					Platform: platform,
					Arch:     arch,
					Channel:  channel,
					Mode:     mode,
				},
			}
			groups[key] = group
		}
		group.stat.RequestCount++
		if row.Type == LogTypeConsume {
			group.stat.SuccessCount++
		} else {
			group.stat.ErrorCount++
		}
		if latency := uclawVersionLatencyMs(row, other); latency > 0 {
			group.latencies = append(group.latencies, latency)
			group.latencySum += latency
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}

	for _, group := range groups {
		if group.stat.RequestCount > 0 {
			group.stat.SuccessRate = float64(group.stat.SuccessCount) / float64(group.stat.RequestCount)
			group.stat.ErrorRate = float64(group.stat.ErrorCount) / float64(group.stat.RequestCount)
		}
		if len(group.latencies) > 0 {
			group.stat.AverageLatencyMs = group.latencySum / float64(len(group.latencies))
			group.stat.P95LatencyMs = percentile95(group.latencies)
		}
		summary.TotalRequests += group.stat.RequestCount
		summary.Items = append(summary.Items, group.stat)
	}
	sort.Slice(summary.Items, func(i, j int) bool {
		if summary.Items[i].RequestCount != summary.Items[j].RequestCount {
			return summary.Items[i].RequestCount > summary.Items[j].RequestCount
		}
		if summary.Items[i].Version != summary.Items[j].Version {
			return summary.Items[i].Version > summary.Items[j].Version
		}
		left := summary.Items[i]
		right := summary.Items[j]
		return strings.Join([]string{left.BuildId, left.Commit, left.Platform, left.Arch, left.Channel, left.Mode}, "\x00") >
			strings.Join([]string{right.BuildId, right.Commit, right.Platform, right.Arch, right.Channel, right.Mode}, "\x00")
	})
	return summary, nil
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// buildOpField 构建语言无关的操作描述（写入 Other.op）。
// 前端依据 action(稳定操作标识) + params(结构化参数) 在渲染期用 i18n 本地化展示，
// 因此不在数据库中存储自然语言句子。
func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

// RecordLoginLog 记录用户登录成功的审计日志（type=LogTypeLogin）。
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出/经典前端）；action+params 供前端本地化渲染。
// extra 可携带 login_method、user_agent 等附加信息（普通用户可见）。
func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

// RecordOperationAuditLog 记录管理/高危操作审计日志（type=LogTypeManage）。
// logUserId 为日志归属者（面向用户的操作如额度调整归属目标用户，资源类操作如渠道/系统设置归属操作者），
// username 内部按 logUserId 查询。content 为英文兜底文本（导出/经典前端用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	if other == nil {
		other = make(map[string]interface{})
	}
	replaceClientDiagnostics(c, other)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		recordMinimalUClawVersionSuccess(c, params.UseTimeSeconds)
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	if params.Other == nil {
		params.Other = make(map[string]interface{})
	}
	replaceClientDiagnostics(c, params.Other)
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

type PromptCacheUsageAggregate struct {
	ModelName               string  `json:"model"`
	ChannelID               int     `json:"channel_id"`
	TotalRequests           int64   `json:"total_requests"`
	HitRequests             int64   `json:"hit_requests"`
	RequestHitRate          float64 `json:"request_hit_rate"`
	PromptTokens            int64   `json:"prompt_tokens"`
	CompletionTokens        int64   `json:"completion_tokens"`
	TotalTokens             int64   `json:"total_tokens"`
	CachedTokens            int64   `json:"cached_tokens"`
	TokenCacheRate          float64 `json:"token_cache_rate"`
	TokenCacheRateAvailable bool    `json:"token_cache_rate_available"`
	LastSeenAt              int64   `json:"last_seen_at"`
}

type PromptCacheUsageSummary struct {
	WindowSeconds           int64                       `json:"window_seconds"`
	TotalRequests           int64                       `json:"total_requests"`
	HitRequests             int64                       `json:"hit_requests"`
	RequestHitRate          float64                     `json:"request_hit_rate"`
	PromptTokens            int64                       `json:"prompt_tokens"`
	CompletionTokens        int64                       `json:"completion_tokens"`
	TotalTokens             int64                       `json:"total_tokens"`
	CachedTokens            int64                       `json:"cached_tokens"`
	TokenCacheRate          float64                     `json:"token_cache_rate"`
	TokenCacheRateAvailable bool                        `json:"token_cache_rate_available"`
	LastSeenAt              int64                       `json:"last_seen_at"`
	GeneratedAt             int64                       `json:"generated_at"`
	ScannedRows             int64                       `json:"scanned_rows"`
	TotalAvailableRows      int64                       `json:"total_available_rows"`
	Truncated               bool                        `json:"truncated"`
	ByModel                 []PromptCacheUsageAggregate `json:"by_model"`
	ByChannel               []PromptCacheUsageAggregate `json:"by_channel"`
}

type PromptCacheUsageSummaryFilter struct {
	StartTimestamp int64
	EndTimestamp   int64
	Username       string
	ModelName      string
	ChannelID      int
	Group          string
	Limit          int
	MaxRows        int
}

type promptCacheUsageLogRow struct {
	Id               int
	CreatedAt        int64
	Username         string
	ModelName        string
	ChannelId        int
	PromptTokens     int
	CompletionTokens int
	Other            string
}

const (
	defaultPromptCacheSummaryWindowSeconds = 24 * 60 * 60
	defaultPromptCacheSummaryLimit         = 12
	defaultPromptCacheSummaryMaxRows       = 50000
	maxPromptCacheSummaryLimit             = 100
	maxPromptCacheSummaryRows              = 200000
)

func GetPromptCacheUsageSummary(filter PromptCacheUsageSummaryFilter) (PromptCacheUsageSummary, error) {
	normalizePromptCacheUsageSummaryFilter(&filter)

	summary := PromptCacheUsageSummary{
		WindowSeconds: filter.EndTimestamp - filter.StartTimestamp,
		GeneratedAt:   time.Now().Unix(),
		ByModel:       []PromptCacheUsageAggregate{},
		ByChannel:     []PromptCacheUsageAggregate{},
	}

	tx := LOG_DB.Model(&Log{}).Where("type = ?", LogTypeConsume)
	if filter.StartTimestamp > 0 {
		tx = tx.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		tx = tx.Where("created_at <= ?", filter.EndTimestamp)
	}
	var err error
	if tx, err = applyExplicitLogTextFilter(tx, "username", filter.Username); err != nil {
		return summary, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", filter.ModelName); err != nil {
		return summary, err
	}
	if filter.ChannelID > 0 {
		tx = tx.Where("channel_id = ?", filter.ChannelID)
	}
	if filter.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", filter.Group)
	}

	var totalAvailable int64
	if err := tx.Session(&gorm.Session{}).Count(&totalAvailable).Error; err != nil {
		common.SysError("failed to count prompt cache usage summary logs: " + err.Error())
		return summary, errors.New("查询 Prompt 缓存统计失败")
	}
	summary.TotalAvailableRows = totalAvailable
	summary.Truncated = totalAvailable > int64(filter.MaxRows)

	var rows []promptCacheUsageLogRow
	if err := tx.Session(&gorm.Session{}).
		Select("id, created_at, username, model_name, channel_id, prompt_tokens, completion_tokens, other").
		Order("created_at desc, id desc").
		Limit(filter.MaxRows).
		Find(&rows).Error; err != nil {
		common.SysError("failed to query prompt cache usage summary logs: " + err.Error())
		return summary, errors.New("查询 Prompt 缓存统计失败")
	}
	summary.ScannedRows = int64(len(rows))

	byModel := map[string]PromptCacheUsageAggregate{}
	byChannel := map[string]PromptCacheUsageAggregate{}
	for _, row := range rows {
		other, _ := common.StrToMap(row.Other)
		cachedTokens := promptCacheUsageInt64(other["cache_tokens"])
		if row.PromptTokens <= 0 && cachedTokens <= 0 {
			continue
		}

		addPromptCacheUsageRow(&summary, row, cachedTokens)

		modelName := strings.TrimSpace(row.ModelName)
		if modelName != "" {
			agg := byModel[modelName]
			if agg.ModelName == "" {
				agg.ModelName = modelName
			}
			addPromptCacheUsageAggregateRow(&agg, row, cachedTokens)
			byModel[modelName] = agg
		}

		if row.ChannelId > 0 {
			channelKey := strconv.Itoa(row.ChannelId)
			agg := byChannel[channelKey]
			if agg.ChannelID == 0 {
				agg.ChannelID = row.ChannelId
			}
			addPromptCacheUsageAggregateRow(&agg, row, cachedTokens)
			byChannel[channelKey] = agg
		}
	}

	finalizePromptCacheUsageSummary(&summary)
	summary.ByModel = finalizePromptCacheUsageAggregateMap(byModel, filter.Limit)
	summary.ByChannel = finalizePromptCacheUsageAggregateMap(byChannel, filter.Limit)

	return summary, nil
}

func normalizePromptCacheUsageSummaryFilter(filter *PromptCacheUsageSummaryFilter) {
	if filter == nil {
		return
	}
	filter.Username = strings.TrimSpace(filter.Username)
	filter.ModelName = strings.TrimSpace(filter.ModelName)
	filter.Group = strings.TrimSpace(filter.Group)
	if filter.EndTimestamp <= 0 {
		filter.EndTimestamp = time.Now().Unix()
	}
	if filter.StartTimestamp <= 0 {
		filter.StartTimestamp = filter.EndTimestamp - defaultPromptCacheSummaryWindowSeconds
	}
	if filter.EndTimestamp < filter.StartTimestamp {
		filter.StartTimestamp, filter.EndTimestamp = filter.EndTimestamp, filter.StartTimestamp
	}
	if filter.EndTimestamp == filter.StartTimestamp {
		filter.EndTimestamp = filter.StartTimestamp + 1
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultPromptCacheSummaryLimit
	}
	if filter.Limit > maxPromptCacheSummaryLimit {
		filter.Limit = maxPromptCacheSummaryLimit
	}
	if filter.MaxRows <= 0 {
		filter.MaxRows = defaultPromptCacheSummaryMaxRows
	}
	if filter.MaxRows > maxPromptCacheSummaryRows {
		filter.MaxRows = maxPromptCacheSummaryRows
	}
}

func addPromptCacheUsageRow(summary *PromptCacheUsageSummary, row promptCacheUsageLogRow, cachedTokens int64) {
	if summary == nil {
		return
	}
	summary.TotalRequests++
	if cachedTokens > 0 {
		summary.HitRequests++
	}
	summary.PromptTokens += int64(row.PromptTokens)
	summary.CompletionTokens += int64(row.CompletionTokens)
	summary.TotalTokens += int64(row.PromptTokens + row.CompletionTokens)
	summary.CachedTokens += cachedTokens
	if row.CreatedAt > summary.LastSeenAt {
		summary.LastSeenAt = row.CreatedAt
	}
}

func addPromptCacheUsageAggregateRow(agg *PromptCacheUsageAggregate, row promptCacheUsageLogRow, cachedTokens int64) {
	if agg == nil {
		return
	}
	agg.TotalRequests++
	if cachedTokens > 0 {
		agg.HitRequests++
	}
	agg.PromptTokens += int64(row.PromptTokens)
	agg.CompletionTokens += int64(row.CompletionTokens)
	agg.TotalTokens += int64(row.PromptTokens + row.CompletionTokens)
	agg.CachedTokens += cachedTokens
	if row.CreatedAt > agg.LastSeenAt {
		agg.LastSeenAt = row.CreatedAt
	}
}

func finalizePromptCacheUsageSummary(summary *PromptCacheUsageSummary) {
	if summary == nil {
		return
	}
	summary.RequestHitRate = promptCacheUsageRequestHitRate(summary.HitRequests, summary.TotalRequests)
	summary.TokenCacheRate, summary.TokenCacheRateAvailable = promptCacheUsageTokenCacheRate(summary.PromptTokens, summary.CachedTokens)
}

func finalizePromptCacheUsageAggregateMap(items map[string]PromptCacheUsageAggregate, limit int) []PromptCacheUsageAggregate {
	out := make([]PromptCacheUsageAggregate, 0, len(items))
	for _, agg := range items {
		agg.RequestHitRate = promptCacheUsageRequestHitRate(agg.HitRequests, agg.TotalRequests)
		agg.TokenCacheRate, agg.TokenCacheRateAvailable = promptCacheUsageTokenCacheRate(agg.PromptTokens, agg.CachedTokens)
		out = append(out, agg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalRequests != out[j].TotalRequests {
			return out[i].TotalRequests > out[j].TotalRequests
		}
		if out[i].CachedTokens != out[j].CachedTokens {
			return out[i].CachedTokens > out[j].CachedTokens
		}
		return out[i].LastSeenAt > out[j].LastSeenAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func promptCacheUsageRequestHitRate(hitRequests, totalRequests int64) float64 {
	if totalRequests <= 0 {
		return 0
	}
	return float64(hitRequests) / float64(totalRequests)
}

func promptCacheUsageTokenCacheRate(promptTokens, cachedTokens int64) (float64, bool) {
	if promptTokens <= 0 {
		return 0, false
	}
	return float64(cachedTokens) / float64(promptTokens), true
}

func promptCacheUsageInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return positiveInt64(int64(v))
	case int64:
		return positiveInt64(v)
	case float64:
		return positiveInt64(int64(v))
	case float32:
		return positiveInt64(int64(v))
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			floatValue, floatErr := strconv.ParseFloat(v.String(), 64)
			if floatErr != nil {
				return 0
			}
			return positiveInt64(int64(floatValue))
		}
		return positiveInt64(parsed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0
		}
		return positiveInt64(int64(parsed))
	default:
		return 0
	}
}

func positiveInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
