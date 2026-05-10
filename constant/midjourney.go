package constant

const (
	MjErrorUnknown = 5
	MjRequestError = 4
)

const (
	MjActionImagine       = "IMAGINE"
	MjActionDescribe      = "DESCRIBE"
	MjActionBlend         = "BLEND"
	MjActionUpscale       = "UPSCALE"
	MjActionVariation     = "VARIATION"
	MjActionReRoll        = "REROLL"
	MjActionInPaint       = "INPAINT"
	MjActionModal         = "MODAL"
	MjActionZoom          = "ZOOM"
	MjActionCustomZoom    = "CUSTOM_ZOOM"
	MjActionShorten       = "SHORTEN"
	MjActionHighVariation = "HIGH_VARIATION"
	MjActionLowVariation  = "LOW_VARIATION"
	MjActionPan           = "PAN"
	MjActionSwapFace      = "SWAP_FACE"
	MjActionUpload        = "UPLOAD"
	MjActionVideo         = "VIDEO"
	MjActionEdits         = "EDITS"
)

var MidjourneyModel2Action = map[string]string{
	"mj_imagine":       MjActionImagine,
	"mj_relax_imagine": MjActionImagine,
	"mj_fast_imagine":  MjActionImagine,
	"mj_turbo_imagine": MjActionImagine,

	"mj_describe":       MjActionDescribe,
	"mj_relax_describe": MjActionDescribe,
	"mj_fast_describe":  MjActionDescribe,
	"mj_turbo_describe": MjActionDescribe,

	"mj_blend":       MjActionBlend,
	"mj_relax_blend": MjActionBlend,
	"mj_fast_blend":  MjActionBlend,
	"mj_turbo_blend": MjActionBlend,

	"mj_upscale":       MjActionUpscale,
	"mj_relax_upscale": MjActionUpscale,
	"mj_fast_upscale":  MjActionUpscale,
	"mj_turbo_upscale": MjActionUpscale,

	"mj_variation":       MjActionVariation,
	"mj_relax_variation": MjActionVariation,
	"mj_fast_variation":  MjActionVariation,
	"mj_turbo_variation": MjActionVariation,

	"mj_reroll":       MjActionReRoll,
	"mj_relax_reroll": MjActionReRoll,
	"mj_fast_reroll":  MjActionReRoll,
	"mj_turbo_reroll": MjActionReRoll,

	"mj_modal":   MjActionModal,
	"mj_inpaint": MjActionInPaint,

	"mj_zoom":       MjActionZoom,
	"mj_relax_zoom": MjActionZoom,
	"mj_fast_zoom":  MjActionZoom,
	"mj_turbo_zoom": MjActionZoom,

	"mj_custom_zoom": MjActionCustomZoom,

	"mj_shorten":       MjActionShorten,
	"mj_relax_shorten": MjActionShorten,
	"mj_fast_shorten":  MjActionShorten,
	"mj_turbo_shorten": MjActionShorten,

	"mj_high_variation":       MjActionHighVariation,
	"mj_relax_high_variation": MjActionHighVariation,
	"mj_fast_high_variation":  MjActionHighVariation,
	"mj_turbo_high_variation": MjActionHighVariation,

	"mj_low_variation":       MjActionLowVariation,
	"mj_relax_low_variation": MjActionLowVariation,
	"mj_fast_low_variation":  MjActionLowVariation,
	"mj_turbo_low_variation": MjActionLowVariation,

	"mj_pan":       MjActionPan,
	"mj_relax_pan": MjActionPan,
	"mj_fast_pan":  MjActionPan,
	"mj_turbo_pan": MjActionPan,

	"swap_face":       MjActionSwapFace,
	"swap_face_relax": MjActionSwapFace,
	"swap_face_fast":  MjActionSwapFace,
	"swap_face_turbo": MjActionSwapFace,

	"mj_upload":       MjActionUpload,
	"mj_relax_upload": MjActionUpload,
	"mj_fast_upload":  MjActionUpload,
	"mj_turbo_upload": MjActionUpload,

	"mj_video":       MjActionVideo,
	"mj_relax_video": MjActionVideo,
	"mj_fast_video":  MjActionVideo,
	"mj_turbo_video": MjActionVideo,

	"mj_edits":       MjActionEdits,
	"mj_relax_edits": MjActionEdits,
	"mj_fast_edits":  MjActionEdits,
	"mj_turbo_edits": MjActionEdits,
}
