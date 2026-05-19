package sora

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

var seedance2ModelSet = map[string]struct{}{
	"doubao-seedance-2-0-260128":      {},
	"doubao-seedance-2-0-fast-260128": {},
}

func IsSeedance2Model(name string) bool {
	_, ok := seedance2ModelSet[name]
	return ok
}

func estimateSeedance2Ratios(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	ratios := map[string]float64{}
	hasVideo := hasVideoInRequest(c, &req)
	if hasVideo {
		if r, ok := GetVideoInputRatio(info.OriginModelName); ok {
			ratios["video_input"] = r
		}
	}
	if isResolution1080p(c, &req) {
		if r, ok := Get1080pRatio(info.OriginModelName, hasVideo); ok {
			ratios["resolution"] = r
		}
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

var seedance2VideoInputRatio = map[string]float64{
	"doubao-seedance-2-0-260128":      28.0 / 46.0,
	"doubao-seedance-2-0-fast-260128": 22.0 / 37.0,
}

var seedance2Resolution1080pRatio = map[string]map[bool]float64{
	"doubao-seedance-2-0-260128": {
		false: 51.0 / 46.0,
		true:  31.0 / 28.0,
	},
}

func GetVideoInputRatio(model string) (float64, bool) {
	r, ok := seedance2VideoInputRatio[model]
	return r, ok
}

func Get1080pRatio(model string, hasVideo bool) (float64, bool) {
	m, ok := seedance2Resolution1080pRatio[model]
	if !ok {
		return 0, false
	}
	r, ok := m[hasVideo]
	return r, ok
}

func hasVideoInRequest(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	if req != nil && req.Metadata != nil {
		if contentInMetadataHasVideo(req.Metadata["content"]) {
			return true
		}
	}
	if raw, ok := peekRawBody(c); ok {
		var top struct {
			Content []map[string]interface{} `json:"content"`
		}
		if err := common.Unmarshal(raw, &top); err == nil {
			for _, item := range top.Content {
				if item["type"] == "video_url" {
					return true
				}
				if _, has := item["video_url"]; has {
					return true
				}
			}
		}
	}
	return false
}

func contentInMetadataHasVideo(raw interface{}) bool {
	arr, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "video_url" {
			return true
		}
		if _, has := m["video_url"]; has {
			return true
		}
	}
	return false
}

// HasImageInRequest 检测 Seedance 2.0 这类走 OpenAI content[] 风格的请求
// 是否带有图片输入。优先复用 TaskSubmitReq.HasImage()（涵盖
// images / image_reference / image_references），未命中再扫描 metadata.content
// 与 raw body 中 type=image_url 的项。
func HasImageInRequest(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	if req != nil && req.HasImage() {
		return true
	}
	if req != nil && req.Metadata != nil {
		if contentInMetadataHasImage(req.Metadata["content"]) {
			return true
		}
	}
	if raw, ok := peekRawBody(c); ok {
		var top struct {
			Content []map[string]interface{} `json:"content"`
		}
		if err := common.Unmarshal(raw, &top); err == nil {
			for _, item := range top.Content {
				if item["type"] == "image_url" {
					return true
				}
				if _, has := item["image_url"]; has {
					return true
				}
			}
		}
	}
	return false
}

func contentInMetadataHasImage(raw interface{}) bool {
	arr, ok := raw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "image_url" {
			return true
		}
		if _, has := m["image_url"]; has {
			return true
		}
	}
	return false
}

func isResolution1080p(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	if req != nil && req.Metadata != nil {
		if is1080pValue(req.Metadata["resolution"]) {
			return true
		}
	}
	if req != nil && is1080pString(req.Size) {
		return true
	}
	if raw, ok := peekRawBody(c); ok {
		var top struct {
			Resolution string `json:"resolution"`
		}
		if err := common.Unmarshal(raw, &top); err == nil && is1080pString(top.Resolution) {
			return true
		}
	}
	return false
}

func is1080pValue(v interface{}) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return is1080pString(s)
}

func is1080pString(s string) bool {
	return s == "1080p" || s == "1080P" || s == "1920x1080"
}

func peekRawBody(c *gin.Context) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}
