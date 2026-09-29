package minimaximage

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct{}

func (a *Adaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// Resolve alias here too: pass-through mode skips ConvertImageRequest,
	// so this is the only place guaranteed to run before the upstream call.
	model := resolveModel(info.UpstreamModelName)
	if model == "" {
		return "", errors.New("minimax image: model name is required")
	}
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		return fmt.Sprintf("%s/v1/content/models/%s/edits", info.ChannelBaseUrl, model), nil
	}
	return fmt.Sprintf("%s/v1/content/models/%s/generations", info.ChannelBaseUrl, model), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("Authorization", "Bearer "+info.ApiKey)
	header.Set("Content-Type", "application/json")
	// Always submit asynchronously and poll: the synchronous endpoint can hang
	// indefinitely (reproducibly so for quality=high), whereas async submission
	// returns a task_id in about a second.
	header.Set("X-Async", "true")
	header.Set("X-Timeout", "600")
	return nil
}

func (a *Adaptor) ConvertImageRequest(_ *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	// Apply alias mapping for upstream; billing stays on the original model name.
	info.UpstreamModelName = resolveModel(info.UpstreamModelName)
	r := buildSyncRequest(request, info.RelayMode)
	r.Model = info.UpstreamModelName
	return r, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return doAsyncImageResponse(c, resp, info)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertRerankRequest(_ *gin.Context, _ int, _ dto.RerankRequest) (any, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertEmbeddingRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertAudioRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not supported")
}
func (a *Adaptor) ConvertGeminiRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not supported")
}
