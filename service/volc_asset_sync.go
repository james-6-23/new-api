package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// 素材库对账与同步。
//
// 为什么需要同步:落库发生在转发成功之后,而「上游已建好、本地记账失败」这条
// 缝隙无法消除(报错会让客户以为没建成而重试,凭空多占一份额度)。于是必须有
// 一条反向路径,把上游有、本地无的素材认领进账本 —— 否则它们对客户不可见、
// 对超管也不可见,却实打实占着 IAM 账号的素材库额度。

// VolcAssetQuotaItem 是额度页上一个渠道的一行。
type VolcAssetQuotaItem struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	// QuotaLimit 管理员手填的条数上限,0 表示未知/不限 —— 官方没有查限额的接口。
	QuotaLimit int `json:"quota_limit"`
	// UpstreamAssets / UpstreamGroups 官方权威总数(含孤儿)。
	UpstreamAssets int `json:"upstream_assets"`
	UpstreamGroups int `json:"upstream_groups"`
	// LocalAssets / LocalGroups 本地记账数。
	LocalAssets int64 `json:"local_assets"`
	LocalGroups int64 `json:"local_groups"`
	// OrphanAssets / OrphanGroups 已认领的无主行数(user_id = 0)。
	OrphanAssets int64 `json:"orphan_assets"`
	OrphanGroups int64 `json:"orphan_groups"`
	// Unrecorded 上游有、本地全无记录的条数,即「还需要跑一次同步」的量。
	// 与 Orphan 分开:前者靠同步消化,后者靠超管清理,页面处置完全不同。
	Unrecorded int `json:"unrecorded"`
	// Error 该渠道查询失败的原因。单渠道失败不影响其余渠道 ——
	// 一个渠道 AK 过期不该让整页打不开。
	Error string `json:"error,omitempty"`
}

// VolcAssetQuotaOverview 汇总各渠道额度。
type VolcAssetQuotaOverview struct {
	Items []*VolcAssetQuotaItem `json:"items"`
}

// GetVolcAssetQuotaOverview 逐个透传渠道对账额度。
//
// 上限来自渠道配置(管理员手填),已用量来自官方 ListAssets 的 TotalCount,
// 本地数来自账本 —— 三者并列展示,超管据此判断是「该清理」还是「该同步」。
func GetVolcAssetQuotaOverview(ctx context.Context) (*VolcAssetQuotaOverview, error) {
	channels, err := model.GetChannelsByType(0, 1000, true, constant.ChannelTypeVolcPassthrough)
	if err != nil {
		return nil, err
	}
	overview := &VolcAssetQuotaOverview{Items: make([]*VolcAssetQuotaItem, 0, len(channels))}
	for _, channel := range channels {
		item := &VolcAssetQuotaItem{ChannelId: channel.Id, ChannelName: channel.Name}
		overview.Items = append(overview.Items, item)

		// 本地数先填:即使官方查不通,超管也该看到本地记了多少。
		item.LocalAssets, _ = model.CountVolcAssetsByChannel(channel.Id, model.VolcResourceTypeAsset)
		item.LocalGroups, _ = model.CountVolcAssetsByChannel(channel.Id, model.VolcResourceTypeAssetGroup)
		item.OrphanAssets, _ = model.CountVolcAssetsByChannelAndOwner(
			channel.Id, model.VolcAssetOrphanOwnerId, model.VolcResourceTypeAsset)
		item.OrphanGroups, _ = model.CountVolcAssetsByChannelAndOwner(
			channel.Id, model.VolcAssetOrphanOwnerId, model.VolcResourceTypeAssetGroup)

		client, err := NewVolcAssetAdminClient(channel)
		if err != nil {
			item.Error = err.Error()
			continue
		}
		item.QuotaLimit = client.QuotaLimit()

		assets, err := client.CountUpstreamAssets(ctx, model.VolcResourceTypeAsset)
		if err != nil {
			item.Error = err.Error()
			continue
		}
		groups, err := client.CountUpstreamAssets(ctx, model.VolcResourceTypeAssetGroup)
		if err != nil {
			item.Error = err.Error()
			continue
		}
		item.UpstreamAssets = assets
		item.UpstreamGroups = groups
		// 差值可能为负:本地行还没被同步删掉,而上游已被别处删除。
		// 负数对超管无意义(该跑同步而不是「还有未记录」),故截到 0。
		item.Unrecorded = maxInt(0, assets-int(item.LocalAssets)) +
			maxInt(0, groups-int(item.LocalGroups))
	}
	return overview, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// VolcAssetSyncResult 一次同步的结果。
type VolcAssetSyncResult struct {
	ChannelId int    `json:"channel_id"`
	Claimed   int    `json:"claimed"`
	Refreshed int    `json:"refreshed"`
	Stale     int    `json:"stale"`
	Error     string `json:"error,omitempty"`
}

// SyncVolcAssetsForChannel 把一个渠道的官方全量与本地账本对齐。
//
// 三种差异分别处置:
//
//	上游有、本地无     认领成 user_id = 0 的无主行 —— 客户查不到(归属校验不匹配),
//	                   超管看得到也删得掉,占额度的素材从此有人管。
//	两边都有           刷新快照(名称/状态可能在官方侧变过),归属保持不动 ——
//	                   同步绝不能改归属,否则等于把客户的素材划给别人。
//	本地有、上游无     只计数不删:官方那边可能是分页抖动或临时错误,
//	                   自动删本地行会让客户的素材凭空消失。交由超管手动确认。
func SyncVolcAssetsForChannel(ctx context.Context, channelId int) (*VolcAssetSyncResult, error) {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return nil, err
	}
	if channel.Type != constant.ChannelTypeVolcPassthrough {
		return nil, fmt.Errorf("渠道 #%d 不是字节火山透传渠道", channelId)
	}
	client, err := NewVolcAssetAdminClient(channel)
	if err != nil {
		return nil, err
	}

	result := &VolcAssetSyncResult{ChannelId: channelId}
	for _, resourceType := range []string{model.VolcResourceTypeAssetGroup, model.VolcResourceTypeAsset} {
		upstream, err := client.ListUpstreamAssets(ctx, resourceType)
		if err != nil {
			return nil, err
		}
		local, err := model.ListVolcAssetsByChannel(channelId, resourceType)
		if err != nil {
			return nil, err
		}

		seen := make(map[string]bool, len(upstream))
		for _, item := range upstream {
			if item.ResourceId == "" {
				continue
			}
			seen[item.ResourceId] = true
			existing, known := local[item.ResourceId]
			userId := model.VolcAssetOrphanOwnerId
			if known {
				userId = existing.UserId
			}
			row := &model.VolcAsset{
				UserId:       userId,
				ChannelId:    channelId,
				ResourceType: resourceType,
				ResourceId:   item.ResourceId,
				GroupId:      item.GroupId,
				Name:         item.Name,
				AssetType:    item.AssetType,
				Status:       item.Status,
			}
			if err := model.RecordVolcAsset(row); err != nil {
				return nil, fmt.Errorf("认领 %s 失败: %w", item.ResourceId, err)
			}
			if known {
				result.Refreshed++
			} else {
				result.Claimed++
			}
		}
		for id := range local {
			if !seen[id] {
				result.Stale++
			}
		}
	}
	return result, nil
}

// AllVolcPassthroughChannelIds 取全部透传渠道 id,供「同步全部」用。
func AllVolcPassthroughChannelIds() ([]int, error) {
	channels, err := model.GetChannelsByType(0, 1000, true, constant.ChannelTypeVolcPassthrough)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.Id)
	}
	return ids, nil
}

// DeleteVolcAssetForAdmin 超管删除一条资产:先删上游,成功后再删本地行。
//
// 顺序不可颠倒。先删本地会在上游删除失败时制造无法挽回的孤儿 —— 上游资产还在、
// 占着额度,本地却没了记录,页面上再也看不到、也没法重试删除。
//
// 上游删除失败时保留本地行并把错误抛给超管:留着行,下次还能重试。
func DeleteVolcAssetForAdmin(ctx context.Context, id int) error {
	asset, err := model.GetVolcAssetById(id)
	if err != nil {
		return err
	}
	channel, err := model.GetChannelById(asset.ChannelId, true)
	if err != nil {
		return fmt.Errorf("资产所属渠道 #%d 不可用: %w", asset.ChannelId, err)
	}
	client, err := NewVolcAssetAdminClient(channel)
	if err != nil {
		return err
	}
	if err := client.DeleteUpstreamAsset(ctx, asset.ResourceType, asset.ResourceId); err != nil {
		return fmt.Errorf("官方删除失败,已保留本地记录: %w", err)
	}
	if err := model.DeleteVolcAssetById(id); err != nil {
		return err
	}
	// 官方 DeleteAssetGroup 会连带删光组内全部素材且不可逆,本地必须跟着级联,
	// 否则那些行永远删不掉(再删官方报「不存在」),页面却还显示它们存在。
	if asset.ResourceType == model.VolcResourceTypeAssetGroup {
		if _, err := model.DeleteVolcAssetsByGroupId(asset.ChannelId, asset.ResourceId); err != nil {
			return fmt.Errorf("素材组已删除,但清理组内本地记录失败: %w", err)
		}
	}
	return nil
}
