package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// VolcAssetSetting 火山素材库的每账号资产条数上限。
//
// 为什么是手填而不是查官方:火山没有查询素材配额的 API —— 全量 Action 里
// 资产相关只有 12 个 CRUD,GetAFPUsage / GetInferenceUsage 都是模型调用量。
// 真实上限在火山控制台「配额管理」里,按主账号计(子账号共享),提额走
// 「配额中心 → 申请配额」。站点这边只能把管理员知道的那个数配进来。
//
// 站点侧的这个上限与渠道配置里的 VolcAssetQuotaLimit 是两回事:
//
//	VolcAssetQuotaLimit  渠道级,对应火山账号的总配额,用于超管对账
//	DefaultAssetLimit    用户级,站点自己分给每个客户的份额,用于限制与展示
type VolcAssetSetting struct {
	// Enabled 是否对创建资产做上限拦截。关闭时上限只展示、不拦截 ——
	// 默认关闭,避免升级后已有客户突然创建失败。
	Enabled bool `json:"enabled"`
	// DefaultAssetLimit 每个账号默认可持有的素材条数上限。0 表示不限。
	DefaultAssetLimit int `json:"default_asset_limit"`
	// CountAssetGroups 统计上限时是否把素材组也算进去。
	// 官方配额把素材与素材组分开计,故默认只算素材。
	CountAssetGroups bool `json:"count_asset_groups"`
}

var volcAssetSetting = VolcAssetSetting{
	Enabled:           false,
	DefaultAssetLimit: 100,
	CountAssetGroups:  false,
}

func init() {
	config.GlobalConfig.Register("volc_asset_setting", &volcAssetSetting)
}

// GetVolcAssetSetting 获取火山素材库配置
func GetVolcAssetSetting() *VolcAssetSetting {
	return &volcAssetSetting
}
