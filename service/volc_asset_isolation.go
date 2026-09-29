package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcpassthrough/assetiso"
)

// 火山透传渠道的资产隔离编排。
//
// 上游一个渠道只有一套 AK/SK,所有站点令牌在火山侧共用同一个资产命名空间,
// 官方 ProjectName 又是 IAM 预建概念、无法按令牌动态开 —— 隔离只能由站点记账。
// 这里把「解析请求/响应」(relay 层纯函数) 与「归属账本」(model 层) 接起来。
//
// 四类校验对应四个入口:
//
//	CheckSingleResourceOwnership  带 Id 的 6 个 Action —— 转发前拒,不发上游
//	CheckCreateAssetGroupRef      CreateAsset 的 GroupId —— 防止往别人的组塞素材
//	CheckAssetReferences          视频提交的 asset:// —— 隔离的闭环
//	FilterListResponseByOwner     List 类 —— 转发后过滤 Items
//
// 加上转发成功后的 RecordCreatedResource / RefreshSnapshot 两个写入口。

// ErrAssetNotOwned 表示目标资源不属于当前用户。
//
// 调用方必须统一按 404 回复,且**不区分**「不存在」与「不属于你」——
// 区分了,站点就成了别人 asset id 的存在性探测器。
var ErrAssetNotOwned = fmt.Errorf("volc asset not found")

// VolcAssetOwner 一次请求的归属上下文。
type VolcAssetOwner struct {
	UserId    int
	ChannelId int
}

// CheckSingleResourceOwnership 校验带 Id 的单资源 Action。
//
// 返回 (false, nil) 表示该 Action 不是单资源类,调用方无需处理;
// 返回 error 即拒绝转发 —— 这类 Action 会读写/删除具体资源,一旦发出上游就晚了。
func CheckSingleResourceOwnership(owner VolcAssetOwner, action string, body []byte) (bool, error) {
	target, ok := assetiso.ParseSingleResourceTarget(action, body)
	if !ok {
		return false, nil
	}
	owned, err := model.IsVolcAssetOwnedBy(owner.UserId, owner.ChannelId, target.ResourceType, target.ResourceId)
	if err != nil {
		return true, fmt.Errorf("校验资产归属失败: %w", err)
	}
	if !owned {
		return true, ErrAssetNotOwned
	}
	return true, nil
}

// CheckCreateAssetGroupRef 校验 CreateAsset 请求体里的 GroupId 归属。
//
// GroupId 为空时放过:官方会自己报缺参错误,站点不代劳参数校验。
func CheckCreateAssetGroupRef(owner VolcAssetOwner, action string, body []byte) error {
	groupId, ok := assetiso.ParseCreateAssetGroupRef(action, body)
	if !ok || groupId == "" {
		return nil
	}
	owned, err := model.IsVolcAssetOwnedBy(owner.UserId, owner.ChannelId,
		model.VolcResourceTypeAssetGroup, groupId)
	if err != nil {
		return fmt.Errorf("校验素材组归属失败: %w", err)
	}
	if !owned {
		return ErrAssetNotOwned
	}
	return nil
}

// CheckAssetReferences 校验视频提交请求体里全部 asset:// 引用的归属。
//
// 这是整套隔离的闭环:少了这一步,客户猜到别人的 asset id 就能直接拿去生成视频,
// 前三类校验全部形同虚设。一个 id 不属于自己就整单拒 —— 不做「过滤掉不属于你的
// 素材再提交」,那会静默改变客户的生成意图。
//
// 返回未通过的 id,供调用方指出具体哪个越权(只回 id 不回归属信息,不泄露他人数据)。
func CheckAssetReferences(owner VolcAssetOwner, body []byte) ([]string, error) {
	ids := assetiso.ExtractAssetIds(body)
	if len(ids) == 0 {
		return nil, nil
	}
	owned, err := model.FilterOwnedVolcAssetIds(owner.UserId, owner.ChannelId,
		model.VolcResourceTypeAsset, ids)
	if err != nil {
		return nil, fmt.Errorf("校验素材归属失败: %w", err)
	}
	var rejected []string
	for _, id := range ids {
		if !owned[id] {
			rejected = append(rejected, id)
		}
	}
	return rejected, nil
}

// FilterListResponseByOwner 过滤 List 类响应,只留属于该用户的条目。
//
// 返回 (新响应体, 是否改写)。改写时调用方必须同步修正 Content-Length ——
// 过滤后长度必然变化,沿用上游首部会让客户端读到截断的 JSON。
//
// 顺带刷新留下来的条目快照:List 响应本来就带 name/status,免费的新鲜度。
func FilterListResponseByOwner(owner VolcAssetOwner, action string, respBody []byte) ([]byte, bool, error) {
	resourceType, ok := assetiso.ListResourceType(action)
	if !ok {
		return respBody, false, nil
	}

	items := assetiso.ParseListedResources(respBody)
	if len(items) == 0 {
		// 没有条目也要过滤:官方可能返回 "Items": [] 或带 TotalCount 的空页,
		// 交给 FilterListResponse 统一处理 TotalCount,行为才一致。
		out, _, changed := assetiso.FilterListResponse(respBody, func(assetiso.ListedResource) bool {
			return false
		})
		return out, changed, nil
	}

	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ResourceId != "" {
			ids = append(ids, item.ResourceId)
		}
	}
	owned, err := model.FilterOwnedVolcAssetIds(owner.UserId, owner.ChannelId, resourceType, ids)
	if err != nil {
		return respBody, false, fmt.Errorf("过滤资产列表失败: %w", err)
	}

	out, _, changed := assetiso.FilterListResponse(respBody, func(item assetiso.ListedResource) bool {
		return owned[item.ResourceId]
	})

	for _, item := range items {
		if owned[item.ResourceId] {
			refreshVolcSnapshot(owner, resourceType, assetiso.CreatedResource{
				ResourceId: item.ResourceId,
				GroupId:    item.GroupId,
				Name:       item.Name,
				AssetType:  item.AssetType,
				Status:     item.Status,
			})
		}
	}
	return out, changed, nil
}

// RecordCreatedResource 转发成功后把新建资源落库。
//
// 只在拿到官方 Id 之后调用。落库失败**不影响**客户的响应:上游资产已经建好了,
// 报错给客户只会让人以为没建成而重试,凭空多占一份额度。代价是产生孤儿
// （上游有、本地无记录,客户看不见但额度照占）—— 故必须打 error 日志带上
// resource_id,超管页面用「上游已用 vs 本地记账」的差值兜底发现。
func RecordCreatedResource(owner VolcAssetOwner, action string, reqBody, respBody []byte) (*model.VolcAsset, error) {
	created, ok := assetiso.ParseCreatedResource(action, reqBody, respBody)
	if !ok {
		return nil, nil
	}
	asset := &model.VolcAsset{
		UserId:       owner.UserId,
		ChannelId:    owner.ChannelId,
		ResourceType: created.ResourceType,
		ResourceId:   created.ResourceId,
		GroupId:      created.GroupId,
		Name:         created.Name,
		AssetType:    created.AssetType,
		Status:       created.Status,
	}
	if err := model.RecordVolcAsset(asset); err != nil {
		return asset, fmt.Errorf("落库资产归属失败: %w", err)
	}
	return asset, nil
}

// RefreshSnapshotFromResponse 用 Get 类响应刷新本地快照。
//
// 只在归属校验已通过后调用 —— 否则等于让任何人拿别人的响应改本地记录。
// 失败只记日志,不影响透传:快照不是权威值,权威值永远回源官方。
func RefreshSnapshotFromResponse(owner VolcAssetOwner, action string, respBody []byte) {
	resourceType, ok := singleResourceType(action)
	if !ok {
		return
	}
	refreshVolcSnapshot(owner, resourceType, assetiso.ParseResourceSnapshot(respBody))
}

// singleResourceType 返回单资源 Action 的资源类型,仅 Get 类需要刷新快照。
func singleResourceType(action string) (string, bool) {
	switch action {
	case "GetAsset":
		return model.VolcResourceTypeAsset, true
	case "GetAssetGroup":
		return model.VolcResourceTypeAssetGroup, true
	}
	return "", false
}

// refreshVolcSnapshot 落一次快照更新。id 为空直接跳过。
func refreshVolcSnapshot(owner VolcAssetOwner, resourceType string, snap assetiso.CreatedResource) {
	if snap.ResourceId == "" {
		return
	}
	_ = model.RecordVolcAsset(&model.VolcAsset{
		UserId:       owner.UserId,
		ChannelId:    owner.ChannelId,
		ResourceType: resourceType,
		ResourceId:   snap.ResourceId,
		GroupId:      snap.GroupId,
		Name:         snap.Name,
		AssetType:    snap.AssetType,
		Status:       snap.Status,
	})
}
