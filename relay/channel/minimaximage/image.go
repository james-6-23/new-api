package minimaximage

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// minimaxImageRequest mirrors the MiniMax /v1/content/models/{model}/generations
// and /v1/content/models/{model}/edits request body.
// Optional scalar fields use pointer + omitempty per Rule 6.
type minimaxImageRequest struct {
	Model             string          `json:"model"`
	Prompt            string          `json:"prompt"`
	N                 *uint           `json:"n,omitempty"`
	Size              string          `json:"size,omitempty"`
	Quality           string          `json:"quality,omitempty"`
	OutputFormat      json.RawMessage `json:"output_format,omitempty"`
	OutputCompression json.RawMessage `json:"output_compression,omitempty"`
	Images            json.RawMessage `json:"images,omitempty"`
	Mask              json.RawMessage `json:"mask,omitempty"`
}

type minimaxBaseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type minimaxImageData struct {
	B64Json       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt"`
}

type minimaxUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func buildSyncRequest(req dto.ImageRequest, relayMode int) minimaxImageRequest {
	r := minimaxImageRequest{
		Model:             req.Model,
		Prompt:            req.Prompt,
		N:                 req.N,
		Size:              req.Size,
		Quality:           req.Quality,
		OutputFormat:      req.OutputFormat,
		OutputCompression: req.OutputCompression,
	}
	if relayMode == relayconstant.RelayModeImagesEdits {
		r.Images = req.Images
		r.Mask = req.Mask
	}
	return r
}
