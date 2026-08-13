package dto

type ChannelSubSupplier struct {
	Name      string  `json:"name"`
	CostRatio float64 `json:"cost_ratio,omitempty"` // 该子供应商自己的 CNY:USD 倍率
}

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
	// CostRatio 渠道成本倍率（CNY : USD）。成本核算用：上游每消耗刊例 $1 记成本 ¥CostRatio。
	// 0/缺省 = 未填写（成本按 0 计并在报表警示），不代表上游免费。
	CostRatio float64 `json:"cost_ratio,omitempty"`
	// CostMode 成本计价方式：""/"ratio"=按倍率（CostRatio，CNY:USD）；"discount"=按成本折扣
	// （成本¥ = 刊例$ × CostDiscount × 查询汇率）。
	CostMode string `json:"cost_mode,omitempty"`
	// CostDiscount 成本折扣（如 0.8 = 刊例价 8 折），仅 CostMode=="discount" 时生效。
	CostDiscount float64 `json:"cost_discount,omitempty"`
	// IsAggregator 聚合渠道标记（背后是自建聚合系统，二期可同步其子供应商成本）。
	IsAggregator bool `json:"is_aggregator,omitempty"`
	// SubSuppliers 聚合渠道背后的子供应商配置（名称+各自倍率）。仅配置与展示；
	// 报表成本仍按渠道级倍率/折扣计算（日志无法归属到子供应商）。
	SubSuppliers []ChannelSubSupplier `json:"sub_suppliers,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型

	// BytePlus 海外素材库（CreateAsset 预上传）。仅海外火山方舟/豆包视频渠道按需启用：
	// 开启后，向上游提交视频生成前会把请求中的参考媒体（公网 URL）预上传到素材库，
	// 拿到 asset://<id> 再发起生成，以稳定走素材库、规避真人脸/内容预过滤拦截。
	BytePlusAssetEnabled   bool   `json:"byteplus_asset_enabled,omitempty"`   // 总开关
	BytePlusAccessKey      string `json:"byteplus_access_key,omitempty"`      // AK（素材库签名用，与视频生成的 Bearer Key 独立）
	BytePlusSecretKey      string `json:"byteplus_secret_key,omitempty"`      // SK
	BytePlusAssetGroupId   string `json:"byteplus_asset_group_id,omitempty"`  // 管理员预建的 GroupId
	BytePlusProjectName    string `json:"byteplus_project_name,omitempty"`    // 资源项目名，默认 "default"
	BytePlusRegion         string `json:"byteplus_region,omitempty"`          // 区域，默认 "ap-southeast-1"
	BytePlusModerationSkip *bool  `json:"byteplus_moderation_skip,omitempty"` // 是否跳过内容预过滤，默认 true（Skip）
	AssetProvider          string `json:"asset_provider,omitempty"`           // 选择素材库协议实现；空值等价 AssetProviderBytePlus，保证存量渠道行为不变。
	// AssetGroupProvider 记录 BytePlusAssetGroupId 是由哪个素材库实现创建/填写的。
	// 素材组 ID 与素材 ID 一样只对创建它的素材库有意义：把 byteplus 的组 ID 交给 cloudwise
	// 轻则触发轮换覆盖掉管理员手建的组 ID，重则每次请求都报错且无自愈路径。
	// 空值表示"未知"（存量渠道均如此），此时按不冲突处理，继续沿用已存的组 ID。
	AssetGroupProvider string `json:"asset_group_provider,omitempty"`

	// 第三方 Seedance 渠道（model.service-inference.ai）素材库预上传总开关。
	// 开启后提交视频生成前把参考媒体（公网 URL）预上传到素材库，换 asset://<id> 再提交。
	// 鉴权复用渠道 Bearer key；素材组按渠道自动创建/复用。
	Seedance3rdAssetEnabled bool `json:"seedance3rd_asset_enabled,omitempty"`
}

const (
	// AssetProviderBytePlus 火山官方素材库（AK/SK 签名 + byteplusapi.com）。
	AssetProviderBytePlus = "byteplus"
	// AssetProviderCloudwise 第三方素材库（复用渠道 base_url 与 API Key）。
	AssetProviderCloudwise = "cloudwise"

	defaultBytePlusRegion      = "ap-southeast-1"
	defaultBytePlusProjectName = "default"
)

// ResolveBytePlusAsset 返回带默认值的 BytePlus 素材库有效配置，
// 避免默认值散落在调用方。region 默认 ap-southeast-1，project 默认 default，
// skipModeration 默认 true（即传 Moderation.Strategy=Skip）。
func (s *ChannelOtherSettings) ResolveBytePlusAsset() (region, project string, skipModeration bool) {
	region = defaultBytePlusRegion
	project = defaultBytePlusProjectName
	skipModeration = true
	if s == nil {
		return
	}
	if s.BytePlusRegion != "" {
		region = s.BytePlusRegion
	}
	if s.BytePlusProjectName != "" {
		project = s.BytePlusProjectName
	}
	if s.BytePlusModerationSkip != nil {
		skipModeration = *s.BytePlusModerationSkip
	}
	return
}

// ResolveAssetProvider 返回素材库协议实现名。空值与未知值都回落到官方实现，
// 保证存量渠道（无此字段）与配置写错时行为不变。
func (s *ChannelOtherSettings) ResolveAssetProvider() string {
	switch s.AssetProvider {
	case AssetProviderCloudwise:
		return AssetProviderCloudwise
	default:
		return AssetProviderBytePlus
	}
}

// ResolveAssetGroupId 返回对当前生效素材库实现有效的素材组 ID。
// 当 AssetGroupProvider 明确记录了另一个实现时，已存的组 ID 对当前实现无意义，
// 按"未配置"处理，由调用方自动新建一个组，从而避免把对方的组 ID 发给上游
// （既可能直接报错，也可能被轮换逻辑覆盖掉管理员手建的组 ID）。
// AssetGroupProvider 为空表示未知（存量渠道），不视为冲突，沿用已存的组 ID。
func (s *ChannelOtherSettings) ResolveAssetGroupId() string {
	if s == nil {
		return ""
	}
	if s.AssetGroupProvider != "" && s.AssetGroupProvider != s.ResolveAssetProvider() {
		return ""
	}
	return s.BytePlusAssetGroupId
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
