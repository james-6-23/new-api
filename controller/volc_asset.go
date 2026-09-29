package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 火山透传素材库的管理接口。
//
// 三种视角:
//
//	自己的资产   任何登录用户,只看得到 user_id 与自己相同的行
//	全部资产     超管,可按用户/渠道/类型/状态/关键字筛
//	额度与同步   超管,回源官方对账并认领孤儿
//
// 客户视角的资产在本地账本里就是权威的 —— 账本正是隔离的依据,不需要为了列表
// 再回源官方一次。回源只发生在超管的对账/同步/删除路径上。

// volcAssetDto 是返回给前端的一行资产。
//
// 不直接返回 model.VolcAsset:超管视图要带用户名,而账本表里只有 user_id。
type volcAssetDto struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	Username     string `json:"username,omitempty"`
	ChannelId    int    `json:"channel_id"`
	ResourceType string `json:"resource_type"`
	ResourceId   string `json:"resource_id"`
	GroupId      string `json:"group_id"`
	Name         string `json:"name"`
	AssetType    string `json:"asset_type"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// volcAssetsToDto 转换列表。fillUser 为 true 时补用户名(仅超管视图需要)。
//
// 用户名按 user_id 去重后查,避免一页 20 行打 20 次缓存;查不到名字不算失败 ——
// 用户可能已被删除,而资产还在官方那边占着额度,这行必须仍然显示出来。
func volcAssetsToDto(assets []*model.VolcAsset, fillUser bool) []*volcAssetDto {
	out := make([]*volcAssetDto, 0, len(assets))
	names := make(map[int]string)
	for _, a := range assets {
		item := &volcAssetDto{
			Id:           a.Id,
			UserId:       a.UserId,
			ChannelId:    a.ChannelId,
			ResourceType: a.ResourceType,
			ResourceId:   a.ResourceId,
			GroupId:      a.GroupId,
			Name:         a.Name,
			AssetType:    a.AssetType,
			Status:       a.Status,
			CreatedAt:    a.CreatedAt,
			UpdatedAt:    a.UpdatedAt,
		}
		if fillUser && a.UserId > 0 {
			if name, ok := names[a.UserId]; ok {
				item.Username = name
			} else if name, err := model.GetUsernameById(a.UserId, false); err == nil {
				names[a.UserId] = name
				item.Username = name
			}
		}
		out = append(out, item)
	}
	return out
}

// parseVolcAssetFilter 解析两个视角共用的筛选参数。
//
// 不解析 user_id / channel_id / keyword:那三个只有超管视角认,调用方各自补。
// 时间戳单位是秒,与 created_at 以及任务日志前端的约定一致;common 里没有
// String2Int64,故直接用 strconv.ParseInt,解析失败按「不筛」处理。
func parseVolcAssetFilter(c *gin.Context) model.VolcAssetFilter {
	return model.VolcAssetFilter{
		ResourceType:   c.Query("resource_type"),
		Status:         c.Query("status"),
		ResourceId:     c.Query("resource_id"),
		StartTimestamp: parseVolcTimestamp(c.Query("start_timestamp")),
		EndTimestamp:   parseVolcTimestamp(c.Query("end_timestamp")),
	}
}

func parseVolcTimestamp(raw string) int64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// GetSelfVolcAssets 返回当前用户在本站点创建的资产。
//
// user_id 只取自认证上下文,绝不接受 query 传入 —— 否则任何登录用户都能翻别人的账本。
func GetSelfVolcAssets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	filter := parseVolcAssetFilter(c)
	// 覆盖而非信任:即使 query 里带了 user_id,也一律换成当前登录用户。
	filter.UserId = c.GetInt("id")

	assets, total, err := model.GetVolcAssetsByFilter(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(volcAssetsToDto(assets, false))
	common.ApiSuccess(c, pageInfo)
}

// GetAllVolcAssets 超管视角的全部资产,支持按用户/渠道/类型/状态/关键字筛。
//
// 不带 user_id 过滤时也会带出 user_id = 0 的孤儿行 —— 那正是超管需要看见并清理的。
func GetAllVolcAssets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter := parseVolcAssetFilter(c)
	filter.UserId = common.String2Int(c.Query("user_id"))
	filter.ChannelId = common.String2Int(c.Query("channel_id"))
	filter.Keyword = c.Query("keyword")

	assets, total, err := model.GetVolcAssetsByFilter(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(volcAssetsToDto(assets, true))
	common.ApiSuccess(c, pageInfo)
}

// selfVolcAssetQuotaDto 是客户视角的额度,只讲「我能建多少、还剩多少」。
//
// 不复用超管的对账结构:那里有孤儿数、上游总数与渠道名,都是站点内部账,
// 泄漏给客户既无意义又暴露了别人也在用同一个火山账号。
type selfVolcAssetQuotaDto struct {
	// Limit 站点分配给该账号的素材条数上限,0 表示不限。
	Limit int `json:"limit"`
	// Used 已持有条数。
	Used int64 `json:"used"`
	// Enforced 上限是否实际生效。false 时上限只是展示值,不会拦截创建。
	Enforced bool `json:"enforced"`
	// CountAssetGroups 素材组是否计入上限,决定前端文案措辞。
	CountAssetGroups bool `json:"count_asset_groups"`
}

// GetSelfVolcAssetQuota 返回当前用户的素材额度。
//
// 已用量按渠道分别算,页面上展示的是各渠道之和 —— 客户并不知道站点背后挂了
// 几个火山账号,给他一个跨渠道的总数才对得上他自己数出来的条数。
func GetSelfVolcAssetQuota(c *gin.Context) {
	userId := c.GetInt("id")
	setting := operation_setting.GetVolcAssetSetting()

	types := []string{model.VolcResourceTypeAsset}
	if setting.CountAssetGroups {
		types = append(types, model.VolcResourceTypeAssetGroup)
	}
	used, err := model.CountVolcAssetsByUserAllChannels(userId, types)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, selfVolcAssetQuotaDto{
		Limit:            service.ResolveVolcAssetLimit(userId),
		Used:             used,
		Enforced:         setting.Enabled,
		CountAssetGroups: setting.CountAssetGroups,
	})
}

// GetVolcAssetQuota 返回各渠道的素材额度对账。
//
// 上限是管理员在渠道里手填的(官方没有查限额的 API),已用量回源官方 ListAssets
// 的 TotalCount,本地记账与孤儿数取自账本。单渠道查询失败只在该行标 error,
// 不让整页失败 —— 一个渠道 AK 过期不该挡住其余渠道的对账。
func GetVolcAssetQuota(c *gin.Context) {
	overview, err := service.GetVolcAssetQuotaOverview(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

// SyncVolcAssets 把官方全量与本地账本对齐。
//
// 不传 channel_id 即同步全部透传渠道。单渠道失败记在该渠道的结果行里继续下一个:
// 同步是幂等的,失败的渠道下次再点一次就行,没必要因为一个渠道放弃全部。
func SyncVolcAssets(c *gin.Context) {
	channelId := common.String2Int(c.Query("channel_id"))
	ids := []int{channelId}
	if channelId <= 0 {
		all, err := service.AllVolcPassthroughChannelIds()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		ids = all
	}

	results := make([]*service.VolcAssetSyncResult, 0, len(ids))
	for _, id := range ids {
		result, err := service.SyncVolcAssetsForChannel(c.Request.Context(), id)
		if err != nil {
			results = append(results, &service.VolcAssetSyncResult{ChannelId: id, Error: err.Error()})
			continue
		}
		results = append(results, result)
	}
	common.ApiSuccess(c, gin.H{"items": results})
}

// DeleteVolcAsset 超管删除一条资产:先删官方,成功后再删本地行。
//
// 顺序不可颠倒,失败时本地行必须留着 —— 详见 service.DeleteVolcAssetForAdmin。
func DeleteVolcAsset(c *gin.Context) {
	id := common.String2Int(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的资产记录 id")
		return
	}
	if err := service.DeleteVolcAssetForAdmin(c.Request.Context(), id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
