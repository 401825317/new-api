package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	// This local compatibility retry is terminal on failure: it must not turn
	// into a cross-channel replay through the controller's generic retry loop.
	defer func() {
		if newAPIError != nil && c.GetBool("responses_thinking_fallback_used") {
			types.ErrOptionWithSkipRetry()(newAPIError)
		}
	}()
	info.InitChannelMeta(c)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		switch info.ApiType {
		case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		default:
			return types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = &dto.OpenAIResponsesRequest{
			Model:              req.Model,
			Input:              req.Input,
			Instructions:       req.Instructions,
			PreviousResponseID: req.PreviousResponseID,
		}
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	var requestBody io.Reader
	var fallbackBody []byte
	// Responses recovery must always pass through the adaptor conversion layer.
	// Raw-body passthrough would bypass provider-bound state sanitization and
	// reintroduce encrypted-content failures on fallback attempts.
	responsesPassthroughForbidden := info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact
	if !responsesPassthroughForbidden && (model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for OpenAI Responses API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		if common.DebugEnabled {
			println("requestBody: ", string(jsonData))
		}
		requestBody = bytes.NewBuffer(jsonData)
		fallbackBody = jsonData
	}

	var httpResp *http.Response
	resp, err := responsesRequestWithThinkingFallback(c, info, requestBody, fallbackBody, func(body io.Reader) (any, error) {
		return adaptor.DoRequest(c, info, body)
	})
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	if resp != nil {
		httpResp = resp.(*http.Response)

		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usageDto := usage.(*dto.Usage)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		originModelName := info.OriginModelName
		originPriceData := info.PriceData

		_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			info.OriginModelName = originModelName
			info.PriceData = originPriceData
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		service.PostTextConsumeQuota(c, info, usageDto, nil)

		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usageDto, "")
	} else {
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}
	return nil
}

// responsesRequestWithThinkingFallback retries only a rejected HTTP request on
// the already selected adaptor/credentials. It never reselects a channel or
// mutates continuation/tool history. SSE failures lack sufficient evidence here
// and are deliberately left to the existing response handler.
func responsesRequestWithThinkingFallback(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader, payload []byte, send func(io.Reader) (any, error)) (any, error) {
	resp, err := send(body)
	if err != nil || c == nil || info == nil || info.ChannelMeta == nil || c.Request.Context().Err() != nil || c.Writer.Written() {
		return resp, err
	}
	if info.RelayMode != relayconstant.RelayModeResponses || info.ApiType != appconstant.APITypeDeepSeek || info.ChannelType != appconstant.ChannelTypeDeepSeek || c.GetBool("responses_thinking_fallback_used") {
		return resp, nil
	}
	if info.ReceivedResponseCount != 0 || (info.ResponsesRecovery != nil && info.ResponsesRecovery.Committed) {
		return resp, nil
	}
	// Only the native V4 adaptor defines Responses thinking-off as effort=none.
	// A DeepSeek-looking alias on an arbitrary compatible gateway is not proof
	// that the gateway implements this contract.
	modelName := gjson.GetBytes(payload, "model").String()
	if !gjson.ValidBytes(payload) || !reasoning.IsDeepSeekV4Model(modelName) || gjson.GetBytes(payload, "reasoning.effort").String() == "none" || gjson.GetBytes(payload, "thinking").Exists() || gjson.GetBytes(payload, "enable_thinking").Exists() {
		return resp, nil
	}
	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil || httpResp.StatusCode != http.StatusBadRequest || httpResp.Body == nil {
		return resp, nil
	}
	const limit = 64 << 10
	originalBody := httpResp.Body
	errorBody, readErr := io.ReadAll(io.LimitReader(originalBody, limit+1))
	// Restore every inspected byte for the ordinary error handler, including
	// oversized/malformed bodies. Keep ownership of the original closer.
	httpResp.Body = &responsesFallbackBody{Reader: io.MultiReader(bytes.NewReader(errorBody), originalBody), Closer: originalBody}
	if readErr != nil || len(errorBody) > limit || !responsesThinkingRejection(errorBody) || c.Request.Context().Err() != nil {
		return resp, nil
	}
	disabled, patchErr := sjson.SetBytes(payload, "reasoning.effort", "none")
	if patchErr != nil {
		return resp, nil
	}
	_ = httpResp.Body.Close()
	c.Set("responses_thinking_fallback_used", true)
	info.ReasoningEffort = "none"
	info.BeginChannelAttempt()
	return send(bytes.NewReader(disabled))
}

// responsesFallbackBody preserves response-body cleanup when inspection must
// fall back to normal error handling; response contents must never be logged here.
type responsesFallbackBody struct {
	io.Reader
	io.Closer
}

// responsesThinkingRejection accepts only an error-only envelope. Unknown
// fields, any usage (even zero), or output fail closed instead of risking replay
// of work already executed upstream. Only the protocol's specific missing-state
// message authorizes changing the requested reasoning mode.
func responsesThinkingRejection(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() || !root.Get("error").IsObject() {
		return false
	}
	allowed := true
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "error", "request_id", "status", "status_code", "type":
		default:
			allowed = false
		}
		return allowed
	})
	root.Get("error").ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "message", "type", "code", "param":
		default:
			allowed = false
		}
		return allowed
	})
	message := strings.ToLower(strings.TrimSpace(root.Get("error.message").String()))
	return allowed && strings.Contains(message, "the reasoning_text in the thinking mode must be passed back to the api")
}
