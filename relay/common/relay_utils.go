package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type HasPrompt interface {
	GetPrompt() string
}

type HasImage interface {
	HasImage() bool
}

func GetFullRequestURL(baseURL string, requestURL string, channelType int) string {
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		switch channelType {
		case constant.ChannelTypeOpenAI:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/v1"))
		case constant.ChannelTypeAzure:
			fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
		}
	}
	return fullRequestURL
}

func GetAPIVersion(c *gin.Context) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion == "" {
		apiVersion = c.GetString("api_version")
	}
	return apiVersion
}

func createTaskError(err error, code string, statusCode int, localError bool) *dto.TaskError {
	return &dto.TaskError{
		Code:       code,
		Message:    err.Error(),
		StatusCode: statusCode,
		LocalError: localError,
		Error:      err,
	}
}

func storeTaskRequest(c *gin.Context, info *RelayInfo, action string, requestObj TaskSubmitReq) {
	info.Action = action
	c.Set("task_request", requestObj)
}
func GetTaskRequest(c *gin.Context) (TaskSubmitReq, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return TaskSubmitReq{}, fmt.Errorf("request not found in context")
	}
	req, ok := v.(TaskSubmitReq)
	if !ok {
		return TaskSubmitReq{}, fmt.Errorf("invalid task request type")
	}
	return req, nil
}

func validatePrompt(prompt string) *dto.TaskError {
	if strings.TrimSpace(prompt) == "" {
		return createTaskError(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest, true)
	}
	return nil
}

// MaxTaskDurationSeconds caps user-supplied video duration. Duration is used
// as a billing multiplier (OtherRatio "seconds"); an unbounded value could
// overflow quota calculation into a negative charge.
const MaxTaskDurationSeconds = 3600

func validateTaskDurationBounds(req TaskSubmitReq) *dto.TaskError {
	seconds := req.Duration
	if seconds == 0 && req.Seconds != "" {
		seconds, _ = strconv.Atoi(req.Seconds)
	}
	if seconds < 0 || seconds > MaxTaskDurationSeconds {
		return createTaskError(fmt.Errorf("seconds must be between 1 and %d", MaxTaskDurationSeconds), "invalid_seconds", http.StatusBadRequest, true)
	}
	return nil
}

func validateMultipartTaskRequest(c *gin.Context, info *RelayInfo, action string) (TaskSubmitReq, error) {
	var req TaskSubmitReq
	if _, err := c.MultipartForm(); err != nil {
		return req, err
	}

	formData := c.Request.PostForm
	req = TaskSubmitReq{
		Prompt:   formData.Get("prompt"),
		Model:    formData.Get("model"),
		Mode:     formData.Get("mode"),
		Image:    formData.Get("image"),
		Size:     formData.Get("size"),
		Metadata: make(map[string]interface{}),
	}

	if durationStr := formData.Get("seconds"); durationStr != "" {
		if duration, err := strconv.Atoi(durationStr); err == nil {
			req.Duration = duration
		}
	}

	if images := formData["images"]; len(images) > 0 {
		req.Images = images
	}

	// 处理 input_references（多图 Multipart）
	if mf, err := c.MultipartForm(); err == nil {
		if files, exists := mf.File["input_references"]; exists && len(files) > 0 {
			var images []string
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					continue
				}
				fileBytes, err := io.ReadAll(file)
				file.Close()
				if err != nil {
					continue
				}
				// 检测 MIME 类型
				mimeType := http.DetectContentType(fileBytes)
				dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(fileBytes))
				images = append(images, dataURI)
			}
			if len(images) > 0 {
				req.Images = images
			}
		}
	}

	for key, values := range formData {
		if len(values) > 0 && !isKnownTaskField(key) {
			if intVal, err := strconv.Atoi(values[0]); err == nil {
				req.Metadata[key] = intVal
			} else if floatVal, err := strconv.ParseFloat(values[0], 64); err == nil {
				req.Metadata[key] = floatVal
			} else {
				req.Metadata[key] = values[0]
			}
		}
	}
	return req, nil
}

func ValidateMultipartDirect(c *gin.Context, info *RelayInfo) *dto.TaskError {
	var prompt string
	var model string
	var seconds int
	var size string
	var hasInputReference bool

	var req TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_json", http.StatusBadRequest, true)
	}

	prompt = req.Prompt
	model = req.Model
	size = req.Size
	seconds, _ = strconv.Atoi(req.Seconds)
	if seconds == 0 {
		seconds = req.Duration
	}
	// 归一化 image_reference / image_references → Images[]
	if len(req.ImageReferences) > 0 {
		// 多图：提取每个 image_url
		for _, ref := range req.ImageReferences {
			if ref.ImageURL != "" {
				req.Images = append(req.Images, ref.ImageURL)
			}
		}
	} else if req.ImageReference != nil && req.ImageReference.ImageURL != "" {
		// 单图：提取 image_url
		req.Images = append(req.Images, req.ImageReference.ImageURL)
	} else if req.InputReference != "" {
		// 已有逻辑：Multipart 单图
		req.Images = []string{req.InputReference}
	}

	if strings.TrimSpace(req.Model) == "" {
		return createTaskError(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest, true)
	}

	if req.HasImage() {
		hasInputReference = true
	}

	if taskErr := validatePrompt(prompt); taskErr != nil {
		return taskErr
	}

	if taskErr := validateTaskDurationBounds(req); taskErr != nil {
		return taskErr
	}

	action := constant.TaskActionTextGenerate
	if hasInputReference {
		action = constant.TaskActionGenerate
	}
	if strings.HasPrefix(model, "sora-2") {

		if size == "" {
			size = "720x1280"
		}

		if seconds <= 0 {
			seconds = 4
		}

		if model == "sora-2" && !lo.Contains([]string{"720x1280", "1280x720"}, size) {
			return createTaskError(fmt.Errorf("sora-2 size is invalid"), "invalid_size", http.StatusBadRequest, true)
		}
		if model == "sora-2-pro" && !lo.Contains([]string{"720x1280", "1280x720", "1792x1024", "1024x1792"}, size) {
			return createTaskError(fmt.Errorf("sora-2 size is invalid"), "invalid_size", http.StatusBadRequest, true)
		}
		// OtherRatios 已移到 Sora adaptor 的 EstimateBilling 中设置
	}

	storeTaskRequest(c, info, action, req)

	return nil
}

func isKnownTaskField(field string) bool {
	knownFields := map[string]bool{
		"prompt":          true,
		"model":           true,
		"mode":            true,
		"image":           true,
		"images":          true,
		"size":            true,
		"duration":        true,
		"input_reference":  true, // Sora 特有字段
		"image_reference":  true,
		"image_references": true,
		"input_references": true,
		"seconds":          true,
	}
	return knownFields[field]
}

// promoteUnknownFieldsToMetadata 实现 Doubao Seedance 2.0 等上游文档承诺的契约:
// "顶层参数与 metadata 同时传时,metadata 中已有的同名字段优先,顶层字段用于补全"。
//
// 在此之前,JSON 路径只调 UnmarshalBodyReusable(c, &req),TaskSubmitReq 没有声明的顶层字段
// (例如 Seedance 的 ratio / resolution / generate_audio / watermark / seed / camera_fixed /
// safety_identifier / return_last_frame / service_tier / execution_expires_after / content)
// 会被 Go encoding/json 静默丢弃。multipart 路径在 validateMultipartTaskRequest 里已经做了
// 等价合并(见 134-144 行),这里把 JSON 路径补齐到同一行为。
//
// 注意:isKnownTaskField 白名单内的字段(如 duration)不会被升级到 metadata,
// 因为它们已经在 TaskSubmitReq 的对应字段里解出,适配器应直接读 req.Duration 等。
func promoteUnknownFieldsToMetadata(c *gin.Context, req *TaskSubmitReq) {
	var raw map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return
	}
	if req.Metadata == nil {
		req.Metadata = make(map[string]interface{})
	}
	for k, v := range raw {
		// 跳过 TaskSubmitReq 直接识别的顶层字段与 metadata 本身
		if isKnownTaskField(k) || k == "metadata" {
			continue
		}
		// 文档契约:metadata 中已有的同名字段优先,顶层只用于补全
		if _, exists := req.Metadata[k]; exists {
			continue
		}
		var val interface{}
		if err := common.Unmarshal(v, &val); err == nil {
			req.Metadata[k] = val
		}
	}
}

func ValidateBasicTaskRequest(c *gin.Context, info *RelayInfo, action string) *dto.TaskError {
	var err error
	contentType := c.GetHeader("Content-Type")
	var req TaskSubmitReq
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req, err = validateMultipartTaskRequest(c, info, action)
		if err != nil {
			return createTaskError(err, "invalid_multipart_form", http.StatusBadRequest, true)
		}
	}
	// 为了metadata字段的兼容性，统一UnmarshalBodyReusable
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return createTaskError(err, "invalid_request", http.StatusBadRequest, true)
	}

	if taskErr := validatePrompt(req.Prompt); taskErr != nil {
		return taskErr
	}

	if taskErr := validateTaskDurationBounds(req); taskErr != nil {
		return taskErr
	}

	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		// 兼容单图上传
		req.Images = []string{req.Image}
	}

	// 文档契约:顶层未知字段补全到 metadata(metadata 已有同名字段优先)。
	// 修复 Doubao Seedance 2.0 等模型在 JSON 路径下顶层 ratio/resolution/duration/
	// generate_audio/watermark/seed/content 等字段被 Go 默认行为静默丢弃的问题。
	promoteUnknownFieldsToMetadata(c, &req)

	storeTaskRequest(c, info, action, req)
	return nil
}
