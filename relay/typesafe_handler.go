package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/typesafe"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func TypeSafeHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	info.DisablePing = true
	if info.ChannelType != constant.ChannelTypeTypeSafe {
		return types.NewErrorWithStatusCode(errors.New("/v1/systemone requires a TypeSafe channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	original, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.TypeSafeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// Each retry attempt must start from the client payload, not the previous
	// channel's mapped model.
	request := *original
	if err := helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	jsonData, err := common.Marshal(request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
		// Re-validate so a param override cannot turn on streaming.
		var effective dto.TypeSafeRequest
		if err := common.Unmarshal(jsonData, &effective); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeChannelParamOverrideInvalid, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	logger.LogDebug(c, "TypeSafe request body: %s", jsonData)

	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	info.UpstreamRequestBodySize = size

	adaptor := &typesafe.Adaptor{}
	adaptor.Init(info)
	resp, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		return types.NewErrorWithStatusCode(errors.New("invalid TypeSafe response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	usage, apiErr := adaptor.DoResponse(c, httpResp, info)
	if apiErr != nil {
		return apiErr
	}
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}
