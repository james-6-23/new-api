package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

func TestGetMjRequestModelImagineSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "explicit relax model", request: dto.MidjourneyRequest{Model: "mj_relax_imagine"}, want: "mj_relax_imagine"},
		{name: "explicit fast model", request: dto.MidjourneyRequest{Model: "mj_fast_imagine"}, want: "mj_fast_imagine"},
		{name: "explicit turbo model", request: dto.MidjourneyRequest{Model: "mj_turbo_imagine"}, want: "mj_turbo_imagine"},
		{name: "legacy imagine model", request: dto.MidjourneyRequest{Model: "mj_imagine"}, want: "mj_imagine"},
		{name: "relax mode", request: dto.MidjourneyRequest{Mode: "RELAX"}, want: "mj_relax_imagine"},
		{name: "fast mode", request: dto.MidjourneyRequest{Mode: "FAST"}, want: "mj_fast_imagine"},
		{name: "turbo mode", request: dto.MidjourneyRequest{Mode: "TURBO"}, want: "mj_turbo_imagine"},
		{name: "missing mode defaults to relax", request: dto.MidjourneyRequest{}, want: "mj_relax_imagine"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyImagine, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelImagineRejectsInvalidSpeedInputs(t *testing.T) {
	tests := []dto.MidjourneyRequest{
		{Model: "mj_relax_blend"},
		{Mode: "SLOW"},
	}

	for _, request := range tests {
		if got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyImagine, &request); ok || mjErr == nil || got != "" {
			t.Fatalf("GetMjRequestModel(%+v) = model %q err %v ok %v, want rejection", request, got, mjErr, ok)
		}
	}
}

func TestGetMjRequestModelBlendSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "explicit relax model", request: dto.MidjourneyRequest{Model: "mj_relax_blend"}, want: "mj_relax_blend"},
		{name: "explicit fast model", request: dto.MidjourneyRequest{Model: "mj_fast_blend"}, want: "mj_fast_blend"},
		{name: "explicit turbo model", request: dto.MidjourneyRequest{Model: "mj_turbo_blend"}, want: "mj_turbo_blend"},
		{name: "legacy blend model", request: dto.MidjourneyRequest{Model: "mj_blend"}, want: "mj_blend"},
		{name: "relax mode", request: dto.MidjourneyRequest{Mode: "RELAX"}, want: "mj_relax_blend"},
		{name: "fast mode", request: dto.MidjourneyRequest{Mode: "FAST"}, want: "mj_fast_blend"},
		{name: "turbo mode", request: dto.MidjourneyRequest{Mode: "TURBO"}, want: "mj_turbo_blend"},
		{name: "missing mode defaults to relax", request: dto.MidjourneyRequest{}, want: "mj_relax_blend"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyBlend, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelDescribeSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "explicit fast model", request: dto.MidjourneyRequest{Model: "mj_fast_describe"}, want: "mj_fast_describe"},
		{name: "turbo mode", request: dto.MidjourneyRequest{Mode: "TURBO"}, want: "mj_turbo_describe"},
		{name: "missing mode defaults to relax", request: dto.MidjourneyRequest{}, want: "mj_relax_describe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyDescribe, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelShortenSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "fast mode", request: dto.MidjourneyRequest{Mode: "FAST"}, want: "mj_fast_shorten"},
		{name: "explicit turbo model", request: dto.MidjourneyRequest{Model: "mj_turbo_shorten"}, want: "mj_turbo_shorten"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyShorten, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelVideoSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "fast mode", request: dto.MidjourneyRequest{Mode: "FAST"}, want: "mj_fast_video"},
		{name: "relax model", request: dto.MidjourneyRequest{Model: "mj_relax_video"}, want: "mj_relax_video"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyVideo, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelSwapFaceSpeedModels(t *testing.T) {
	tests := []struct {
		name    string
		request dto.MidjourneyRequest
		want    string
	}{
		{name: "fast mode", request: dto.MidjourneyRequest{Mode: "FAST"}, want: "swap_face_fast"},
		{name: "turbo model", request: dto.MidjourneyRequest{Model: "swap_face_turbo"}, want: "swap_face_turbo"},
		{name: "default relax", request: dto.MidjourneyRequest{}, want: "swap_face_relax"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeSwapFace, &tt.request)
			if !ok || mjErr != nil {
				t.Fatalf("GetMjRequestModel returned ok=%v err=%v", ok, mjErr)
			}
			if got != tt.want {
				t.Fatalf("GetMjRequestModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMjRequestModelRejectsInvalidModel(t *testing.T) {
	request := dto.MidjourneyRequest{Model: "nonexistent_model"}
	if got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyBlend, &request); ok || mjErr == nil || got != "" {
		t.Fatalf("GetMjRequestModel(%+v) = model %q err %v ok %v, want rejection", request, got, mjErr, ok)
	}
}

func TestGetMjRequestModelRejectsInvalidMode(t *testing.T) {
	request := dto.MidjourneyRequest{Mode: "SLOW"}
	if got, mjErr, ok := GetMjRequestModel(relayconstant.RelayModeMidjourneyBlend, &request); ok || mjErr == nil || got != "" {
		t.Fatalf("GetMjRequestModel(%+v) = model %q err %v ok %v, want rejection", request, got, mjErr, ok)
	}
}

func TestGetMjSpeedModelName(t *testing.T) {
	tests := []struct {
		action string
		mode   string
		want   string
	}{
		{constant.MjActionBlend, "RELAX", "mj_relax_blend"},
		{constant.MjActionBlend, "FAST", "mj_fast_blend"},
		{constant.MjActionBlend, "TURBO", "mj_turbo_blend"},
		{constant.MjActionDescribe, "FAST", "mj_fast_describe"},
		{constant.MjActionShorten, "TURBO", "mj_turbo_shorten"},
		{constant.MjActionUpscale, "RELAX", "mj_relax_upscale"},
		{constant.MjActionVariation, "FAST", "mj_fast_variation"},
		{constant.MjActionReRoll, "TURBO", "mj_turbo_reroll"},
		{constant.MjActionVideo, "FAST", "mj_fast_video"},
		{constant.MjActionUpload, "RELAX", "mj_relax_upload"},
		{constant.MjActionSwapFace, "FAST", "swap_face_fast"},
		{constant.MjActionSwapFace, "RELAX", "swap_face_relax"},
		{constant.MjActionSwapFace, "TURBO", "swap_face_turbo"},
		{constant.MjActionHighVariation, "FAST", "mj_fast_high_variation"},
		{constant.MjActionLowVariation, "TURBO", "mj_turbo_low_variation"},
		{constant.MjActionPan, "RELAX", "mj_relax_pan"},
		{constant.MjActionZoom, "FAST", "mj_fast_zoom"},
		{constant.MjActionEdits, "TURBO", "mj_turbo_edits"},
		{constant.MjActionBlend, "", "mj_relax_blend"},
	}

	for _, tt := range tests {
		t.Run(tt.action+"_"+tt.mode, func(t *testing.T) {
			got := GetMjSpeedModelName(tt.action, tt.mode)
			if got != tt.want {
				t.Fatalf("GetMjSpeedModelName(%q, %q) = %q, want %q", tt.action, tt.mode, got, tt.want)
			}
		})
	}
}

func TestIsMjSpeedModel(t *testing.T) {
	speedModels := []string{
		"mj_relax_imagine", "mj_fast_imagine", "mj_turbo_imagine",
		"mj_relax_blend", "mj_fast_blend", "mj_turbo_blend",
		"mj_relax_describe", "mj_fast_high_variation",
		"swap_face_relax", "swap_face_fast", "swap_face_turbo",
	}
	for _, m := range speedModels {
		if !IsMjSpeedModel(m) {
			t.Errorf("IsMjSpeedModel(%q) = false, want true", m)
		}
	}

	nonSpeedModels := []string{
		"mj_imagine", "mj_blend", "mj_describe", "swap_face", "mj_modal",
	}
	for _, m := range nonSpeedModels {
		if IsMjSpeedModel(m) {
			t.Errorf("IsMjSpeedModel(%q) = true, want false", m)
		}
	}
}

func TestGetMjModeFromSpeedModel(t *testing.T) {
	tests := []struct {
		model string
		mode  string
		ok    bool
	}{
		{"mj_relax_blend", "RELAX", true},
		{"mj_fast_describe", "FAST", true},
		{"mj_turbo_shorten", "TURBO", true},
		{"swap_face_relax", "RELAX", true},
		{"swap_face_fast", "FAST", true},
		{"swap_face_turbo", "TURBO", true},
		{"mj_fast_high_variation", "FAST", true},
		{"mj_blend", "", false},
		{"swap_face", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			mode, ok := GetMjModeFromSpeedModel(tt.model)
			if ok != tt.ok || mode != tt.mode {
				t.Fatalf("GetMjModeFromSpeedModel(%q) = (%q, %v), want (%q, %v)", tt.model, mode, ok, tt.mode, tt.ok)
			}
		})
	}
}

func TestNormalizeMidjourneyImagineForwardRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("relay_mode", relayconstant.RelayModeMidjourneyImagine)
	c.Set("original_model", "mj_turbo_imagine")

	body := map[string]interface{}{
		"model":  "mj_turbo_imagine",
		"mode":   "RELAX",
		"prompt": "Cat",
	}
	normalizeMidjourneyForwardRequest(c, body)

	if _, ok := body["model"]; ok {
		t.Fatalf("model should be removed from upstream body")
	}
	if got := body["mode"]; got != "TURBO" {
		t.Fatalf("mode = %v, want TURBO", got)
	}
	if got := body["prompt"]; got != "Cat" {
		t.Fatalf("prompt = %v, want preserved prompt", got)
	}
}

func TestNormalizeMidjourneyForwardRequestBlend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("original_model", "mj_fast_blend")

	body := map[string]interface{}{
		"model": "mj_fast_blend",
		"mode":  "RELAX",
	}
	normalizeMidjourneyForwardRequest(c, body)

	if _, ok := body["model"]; ok {
		t.Fatalf("model should be removed from upstream body")
	}
	if got := body["mode"]; got != "FAST" {
		t.Fatalf("mode = %v, want FAST", got)
	}
}

func TestNormalizeMidjourneyForwardRequestSwapFace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("original_model", "swap_face_turbo")

	body := map[string]interface{}{
		"model": "swap_face_turbo",
		"mode":  "RELAX",
	}
	normalizeMidjourneyForwardRequest(c, body)

	if _, ok := body["model"]; ok {
		t.Fatalf("model should be removed from upstream body")
	}
	if got := body["mode"]; got != "TURBO" {
		t.Fatalf("mode = %v, want TURBO", got)
	}
}

func TestNormalizeMidjourneyForwardRequestNoSpeedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("original_model", "mj_blend")

	body := map[string]interface{}{
		"mode": "RELAX",
	}
	normalizeMidjourneyForwardRequest(c, body)

	if got := body["mode"]; got != "RELAX" {
		t.Fatalf("mode = %v, want RELAX (unchanged)", got)
	}
}

func TestNormalizeMidjourneyImagineForwardRequestSkipsOtherRelayModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("relay_mode", relayconstant.RelayModeMidjourneyBlend)
	c.Set("original_model", "mj_turbo_blend")

	body := map[string]interface{}{
		"model": "mj_turbo_blend",
		"mode":  "RELAX",
	}
	normalizeMidjourneyForwardRequest(c, body)

	if _, ok := body["model"]; ok {
		t.Fatalf("model should be removed for speed model")
	}
	if got := body["mode"]; got != "TURBO" {
		t.Fatalf("mode = %v, want TURBO", got)
	}
}
