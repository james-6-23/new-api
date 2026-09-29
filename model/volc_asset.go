package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 资产类型常量。官方把素材组与素材放在两套 Action 下,归属表用同一张表记账,
// 靠 ResourceType 区分。
const (
	VolcResourceTypeAsset      = "asset"
	VolcResourceTypeAssetGroup = "asset_group"
)

// VolcAsset 记录火山透传渠道下"哪个用户创建了哪个官方资产"。
//
// 为什么需要这张表:上游一个渠道只有一套 AK/SK,所有站点令牌在火山侧共用同一个
// 资产命名空间,官方 ProjectName 又是 IAM 预建概念、无法按令牌动态开。所以隔离
// 只能由站点自己记账 —— 没有这张表,客户猜到别人的 asset id 就能直接读、改、删,
// 甚至拿去生成视频。
//
// 归属粒度是 user_id 而非 token_id:同一用户轮换令牌不该丢素材,跨用户才隔离。
type VolcAsset struct {
	Id     int `json:"id"      gorm:"primaryKey"`
	UserId int `json:"user_id" gorm:"index:idx_volc_asset_owner,priority:1;not null"`
	// ChannelId 上游渠道。不同渠道是不同的火山账号,同名 id 不互通,
	// 故唯一索引与归属校验都必须带上它。
	ChannelId int `json:"channel_id" gorm:"index:idx_volc_asset_owner,priority:2;uniqueIndex:idx_volc_asset_resource,priority:1;not null"`
	// ResourceType asset / asset_group。
	ResourceType string `json:"resource_type" gorm:"type:varchar(32);index:idx_volc_asset_owner,priority:3;uniqueIndex:idx_volc_asset_resource,priority:2;not null"`
	// ResourceId 官方返回的 Id,如 asset-20260410114236-8cdfz。
	ResourceId string `json:"resource_id" gorm:"type:varchar(128);uniqueIndex:idx_volc_asset_resource,priority:3;not null"`
	// GroupId 资产所属素材组(仅 asset 行有值)。
	GroupId string `json:"group_id" gorm:"type:varchar(128)"`
	// 以下四列是官方响应的快照,只为管理页面展示与搜索,不作为权威来源 ——
	// 权威值永远回源官方。校验分支本来就要解析上游响应,顺手刷新,零额外调用。
	Name      string `json:"name"       gorm:"type:varchar(255)"`
	AssetType string `json:"asset_type" gorm:"type:varchar(32)"`
	Status    string `json:"status"     gorm:"type:varchar(32)"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (VolcAsset) TableName() string {
	return "volc_assets"
}

// RecordVolcAsset 落库一条归属记录,已存在则刷新快照。
//
// 只在转发成功、拿到官方 Id 之后调用。用 upsert 而非先查后插:客户可能并发创建,
// 也可能同一 id 被 Get/List 反复刷新快照,冲突走更新比报错更符合语义。
func RecordVolcAsset(asset *VolcAsset) error {
	now := common.GetTimestamp()
	if asset.CreatedAt == 0 {
		asset.CreatedAt = now
	}
	asset.UpdatedAt = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"}, {Name: "resource_type"}, {Name: "resource_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"group_id", "name", "asset_type", "status", "updated_at"}),
	}).Create(asset).Error
}

// IsVolcAssetOwnedBy 判断资源是否属于指定用户的指定渠道。
//
// 三个条件缺一不可:少了 userId 就是跨用户越权,少了 channelId 就是拿 A 渠道的
// 记录给 B 渠道的请求背书(而 B 渠道是另一个火山账号,id 根本不同源)。
func IsVolcAssetOwnedBy(userId, channelId int, resourceType, resourceId string) (bool, error) {
	if resourceId == "" {
		return false, nil
	}
	var count int64
	err := DB.Model(&VolcAsset{}).
		Where("user_id = ? AND channel_id = ? AND resource_type = ? AND resource_id = ?",
			userId, channelId, resourceType, resourceId).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// FilterOwnedVolcAssetIds 从候选 id 中挑出属于该用户的,用一次查询代替 N 次。
//
// 视频提交时要校验 content[] 里的全部 asset://<id>,逐个查表会把一次提交放大成
// 十几次数据库往返。返回集合而非布尔,调用方好指出具体哪个 id 不属于自己。
func FilterOwnedVolcAssetIds(userId, channelId int, resourceType string, ids []string) (map[string]bool, error) {
	owned := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return owned, nil
	}
	var rows []string
	err := DB.Model(&VolcAsset{}).
		Where("user_id = ? AND channel_id = ? AND resource_type = ? AND resource_id IN ?",
			userId, channelId, resourceType, ids).
		Pluck("resource_id", &rows).Error
	if err != nil {
		return nil, err
	}
	for _, id := range rows {
		owned[id] = true
	}
	return owned, nil
}

// GetVolcAssetsByUser 取用户在本站点记账的全部资源,新的在前。
func GetVolcAssetsByUser(userId int, resourceType string, startIdx, num int) ([]*VolcAsset, int64, error) {
	return GetVolcAssetsByFilter(VolcAssetFilter{
		UserId:       userId,
		ResourceType: resourceType,
	}, startIdx, num)
}

// VolcAssetFilter 资产列表的筛选条件,零值表示不筛。
//
// 自己视角与超管视角共用同一个谓词构造器:两边支持的筛选项必须一模一样,
// 否则客户看到的筛选框会有一半点了没反应。差别只在 UserId —— /self 的
// UserId 由认证上下文强制写入,绝不接受 query 传入。
type VolcAssetFilter struct {
	UserId       int
	ChannelId    int
	ResourceType string
	Status       string
	Keyword      string
	// ResourceId 官方资产 id,精确匹配。与 Keyword 的模糊匹配分开:
	// 客户手上拿到的就是完整 id,精确查能直接命中,不会被同前缀的行淹没。
	ResourceId string
	// StartTimestamp / EndTimestamp 创建时间范围,单位秒(与 created_at 同单位,
	// 也与任务日志前端传的单位一致)。0 表示该端不设限。
	StartTimestamp int64
	EndTimestamp   int64
}

// GetVolcAssetsByFilter 按筛选条件取资产列表。
func GetVolcAssetsByFilter(f VolcAssetFilter, startIdx, num int) ([]*VolcAsset, int64, error) {
	query := DB.Model(&VolcAsset{})
	if f.UserId > 0 {
		query = query.Where("user_id = ?", f.UserId)
	}
	if f.ChannelId > 0 {
		query = query.Where("channel_id = ?", f.ChannelId)
	}
	if f.ResourceType != "" {
		query = query.Where("resource_type = ?", f.ResourceType)
	}
	if f.Status != "" {
		query = query.Where("status = ?", f.Status)
	}
	if f.ResourceId != "" {
		query = query.Where("resource_id = ?", f.ResourceId)
	}
	if f.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", f.StartTimestamp)
	}
	if f.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", f.EndTimestamp)
	}
	if f.Keyword != "" {
		// LIKE 在三种数据库上行为一致;JSONB 之类的方言运算符不能用(Rule 2)。
		kw := "%" + f.Keyword + "%"
		query = query.Where("resource_id LIKE ? OR name LIKE ?", kw, kw)
	}
	return paginateVolcAssets(query, startIdx, num)
}

func paginateVolcAssets(query *gorm.DB, startIdx, num int) ([]*VolcAsset, int64, error) {
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var assets []*VolcAsset
	if num <= 0 {
		num = 20
	}
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&assets).Error
	return assets, total, err
}

// GetVolcAssetById 按本地行 id 取记录,供超管删除时先确认渠道与官方 id。
func GetVolcAssetById(id int) (*VolcAsset, error) {
	var asset VolcAsset
	if err := DB.First(&asset, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

// DeleteVolcAssetById 删除本地归属行。
//
// 调用方必须先删上游、成功后才调这里:反过来会制造孤儿 —— 上游资产还在、
// 占着素材库额度,本地却没人认领,谁都看不见也删不掉。
func DeleteVolcAssetById(id int) error {
	return DB.Delete(&VolcAsset{}, "id = ?", id).Error
}

// CountVolcAssetsByChannel 统计各渠道本地记账数,用于和官方 TotalCount 对账。
// 差值即孤儿数。
func CountVolcAssetsByChannel(channelId int, resourceType string) (int64, error) {
	var count int64
	err := DB.Model(&VolcAsset{}).
		Where("channel_id = ? AND resource_type = ?", channelId, resourceType).
		Count(&count).Error
	return count, err
}

// CountVolcAssetsByUser 统计某用户在某渠道下持有的资产条数,用于上限校验。
//
// 按渠道分别计数:不同渠道是不同的火山账号,配额各算各的,合并统计会让
// 在 A 渠道用满的客户在 B 渠道也被拦下。resourceTypes 为空表示统计全部类型。
func CountVolcAssetsByUser(userId, channelId int, resourceTypes []string) (int64, error) {
	query := DB.Model(&VolcAsset{}).Where("user_id = ? AND channel_id = ?", userId, channelId)
	if len(resourceTypes) > 0 {
		query = query.Where("resource_type IN ?", resourceTypes)
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

// CountVolcAssetsByUserAllChannels 统计某用户跨全部渠道的持有量,用于客户侧额度展示。
//
// 与 CountVolcAssetsByUser 的按渠道计数并存:限制是按渠道判的（配额各算各的）,
// 但客户并不知道站点背后挂了几个火山账号,给他看的必须是跨渠道总数。
func CountVolcAssetsByUserAllChannels(userId int, resourceTypes []string) (int64, error) {
	query := DB.Model(&VolcAsset{}).Where("user_id = ?", userId)
	if len(resourceTypes) > 0 {
		query = query.Where("resource_type IN ?", resourceTypes)
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

// VolcAssetOrphanOwnerId 是同步时认领无主行的归属用户 id。
//
// 站点用户 id 从 1 起,0 永远不匹配任何真实用户 —— 于是这些行对客户不可见
// （归属校验查不到）、对超管可见可删（超管列表不带 user 过滤）。上游有、本地
// 无记录的素材正是靠这个归属从「谁都看不见」变成「超管看得见且删得掉」。
const VolcAssetOrphanOwnerId = 0

// CountVolcAssetsByChannelAndOwner 统计指定渠道下归属某个用户的记账行数。
//
// 传 VolcAssetOrphanOwnerId 即统计无主行（孤儿）。分开统计而非在
// CountVolcAssetsByChannel 上做减法:减法算出的是「总分页差值」,无法区分
// 「无主行」与「尚未同步过」两种情况,而这两种情况在页面上的处置完全不同。
func CountVolcAssetsByChannelAndOwner(channelId, userId int, resourceType string) (int64, error) {
	var count int64
	err := DB.Model(&VolcAsset{}).
		Where("channel_id = ? AND user_id = ? AND resource_type = ?", channelId, userId, resourceType).
		Count(&count).Error
	return count, err
}

// ListVolcAssetsByChannel 取某渠道某类型下的全部本地行,按官方 id 建索引。
//
// 同步要拿它和官方 ListAssets 的全量做差集,故不能分页 —— 分页会让未落到
// 当前页的已记录素材被误判成孤儿,进而被重复认领或反复报给超管。
func ListVolcAssetsByChannel(channelId int, resourceType string) (map[string]*VolcAsset, error) {
	var assets []*VolcAsset
	err := DB.Model(&VolcAsset{}).
		Where("channel_id = ? AND resource_type = ?", channelId, resourceType).
		Find(&assets).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]*VolcAsset, len(assets))
	for _, a := range assets {
		out[a.ResourceId] = a
	}
	return out, nil
}

// DeleteVolcAssetsByGroupId 删除某渠道下某素材组关联的全部本地行。
//
// 官方 DeleteAssetGroup 会批量删掉组内所有素材且不可逆,本地若不跟着删,
// 那些行就变成永远删不掉的记录 —— 重新删除单个素材会被官方报「不存在」,
// 而页面还显示它们存在。
func DeleteVolcAssetsByGroupId(channelId int, groupId string) (int64, error) {
	if groupId == "" {
		return 0, nil
	}
	result := DB.Where("channel_id = ? AND group_id = ?", channelId, groupId).Delete(&VolcAsset{})
	return result.RowsAffected, result.Error
}
