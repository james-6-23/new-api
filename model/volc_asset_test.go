package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// volc_assets 不在 model 包 TestMain 的自动迁移列表里（那张列表由无关的
// 测试维护），故本文件自行建表。idempotent，多个测试重复调用安全。
var volcAssetMigrateOnce sync.Once

func ensureVolcAssetTable(t *testing.T) {
	t.Helper()
	volcAssetMigrateOnce.Do(func() {
		if err := DB.AutoMigrate(&VolcAsset{}); err != nil {
			t.Fatalf("migrate volc_assets: %v", err)
		}
	})
	t.Cleanup(func() { DB.Exec("DELETE FROM volc_assets") })
}

func seedVolcAsset(t *testing.T, asset *VolcAsset) *VolcAsset {
	t.Helper()
	require.NoError(t, RecordVolcAsset(asset))
	return asset
}

// ---------------------------------------------------------------------------
// RecordVolcAsset —— upsert 语义
// ---------------------------------------------------------------------------

// 客户可能并发创建，也可能同一 id 被 Get/List 反复刷新快照，冲突走更新比报错
// 更符合语义。刷新时必须就地更新快照字段，而不是留下两行。
func TestRecordVolcAsset_UpsertRefreshesSnapshot(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "asset-1", GroupId: "g1", Name: "old", AssetType: "Image", Status: "Processing",
	})

	// 同一 (channel_id, resource_type, resource_id) 再落一次：应更新而非新增
	require.NoError(t, RecordVolcAsset(&VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "asset-1", GroupId: "g2", Name: "new", AssetType: "Video", Status: "Active",
	}))

	var rows []VolcAsset
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1, "同 id 重复落库必须 upsert，不能留下两行")

	assert.Equal(t, "g2", rows[0].GroupId)
	assert.Equal(t, "new", rows[0].Name)
	assert.Equal(t, "Video", rows[0].AssetType)
	assert.Equal(t, "Active", rows[0].Status)
	assert.NotZero(t, rows[0].CreatedAt)
	assert.GreaterOrEqual(t, rows[0].UpdatedAt, rows[0].CreatedAt)
}

// 归属不可通过 upsert 转移：冲突更新只刷快照，不动 user_id。
//
// 为什么重要：归属校验在转发前完成，能走到落库的只有「自己已拥有」或「自己刚
// 创建」两种情况。若 upsert 顺手改 user_id，任何一条能刷新快照的路径都成了
// 抢注别人资产的口子。
func TestRecordVolcAsset_ConflictDoesNotTransferOwnership(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "asset-1",
	})
	require.NoError(t, RecordVolcAsset(&VolcAsset{
		UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "asset-1",
	}))

	owned, err := IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAsset, "asset-1")
	require.NoError(t, err)
	assert.True(t, owned, "原归属必须保持不变")

	owned, err = IsVolcAssetOwnedBy(2, 10, VolcResourceTypeAsset, "asset-1")
	require.NoError(t, err)
	assert.False(t, owned, "后一次 upsert 不得抢走归属")
}

// 同一官方 id 在两种资源类型下互不干扰（asset 与 asset_group 是两套命名空间）。
func TestRecordVolcAsset_SameIdDifferentResourceTypeCoexist(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "x"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "x"})

	var count int64
	require.NoError(t, DB.Model(&VolcAsset{}).Count(&count).Error)
	assert.EqualValues(t, 2, count)

	owned, err := IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAsset, "x")
	require.NoError(t, err)
	assert.True(t, owned)
	owned, err = IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAssetGroup, "x")
	require.NoError(t, err)
	assert.True(t, owned)
}

// ---------------------------------------------------------------------------
// IsVolcAssetOwnedBy —— 三个条件缺一不可
// ---------------------------------------------------------------------------

// 跨渠道同 id 不互通：不同渠道是不同的火山账号，id 根本不同源。少了 channelId
// 就等于拿 A 渠道的记录给 B 渠道的请求背书。
func TestIsVolcAssetOwnedBy_CrossChannelSameIdNotInterchangeable(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "shared-id",
	})

	owned, err := IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAsset, "shared-id")
	require.NoError(t, err)
	assert.True(t, owned, "本渠道本用户应拥有")

	owned, err = IsVolcAssetOwnedBy(1, 11, VolcResourceTypeAsset, "shared-id")
	require.NoError(t, err)
	assert.False(t, owned, "另一个渠道是另一个火山账号，同 id 不得互通")

	owned, err = IsVolcAssetOwnedBy(2, 10, VolcResourceTypeAsset, "shared-id")
	require.NoError(t, err)
	assert.False(t, owned, "跨用户不得互通")

	owned, err = IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAssetGroup, "shared-id")
	require.NoError(t, err)
	assert.False(t, owned, "跨资源类型不得互通")
}

// 空 id 永远不属于任何人，且不该为此查库报错。
func TestIsVolcAssetOwnedBy_EmptyIdIsNeverOwned(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: ""})

	owned, err := IsVolcAssetOwnedBy(1, 10, VolcResourceTypeAsset, "")
	require.NoError(t, err)
	assert.False(t, owned, "空 id 必须判为不拥有，由归属校验统一 404")
}

// 孤儿归属（user_id = 0）对客户不可见，对超管可见。
func TestIsVolcAssetOwnedBy_OrphanInvisibleToCustomer(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: VolcAssetOrphanOwnerId, ChannelId: 10,
		ResourceType: VolcResourceTypeAsset, ResourceId: "orphan-1",
	})

	// 站点用户 id 从 1 起，0 永远不匹配任何真实用户
	for _, userId := range []int{1, 2, 100} {
		owned, err := IsVolcAssetOwnedBy(userId, 10, VolcResourceTypeAsset, "orphan-1")
		require.NoError(t, err)
		assert.False(t, owned, "孤儿素材对客户 user_id=%d 必须不可见", userId)
	}

	if VolcAssetOrphanOwnerId != 0 {
		t.Fatal("VolcAssetOrphanOwnerId 必须为 0，否则会与真实用户 id 冲突")
	}
}

// ---------------------------------------------------------------------------
// FilterOwnedVolcAssetIds —— 一次查询代替 N 次
// ---------------------------------------------------------------------------

func TestFilterOwnedVolcAssetIds(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "mine-1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "mine-2"})
	seedVolcAsset(t, &VolcAsset{UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "theirs-1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 11, ResourceType: VolcResourceTypeAsset, ResourceId: "other-channel"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "a-group"})

	owned, err := FilterOwnedVolcAssetIds(1, 10, VolcResourceTypeAsset,
		[]string{"mine-1", "mine-2", "theirs-1", "other-channel", "a-group", "nonexistent"})
	require.NoError(t, err)

	assert.True(t, owned["mine-1"])
	assert.True(t, owned["mine-2"])
	assert.False(t, owned["theirs-1"], "别的用户的 id 不得出现")
	assert.False(t, owned["other-channel"], "别的渠道的 id 不得出现")
	assert.False(t, owned["a-group"], "不同资源类型的同 id 不得出现")
	assert.False(t, owned["nonexistent"])
	assert.Len(t, owned, 2)
}

// 空候选集直接返回空 map，不发 `IN ()` 这种在某些数据库上会报错的查询。
func TestFilterOwnedVolcAssetIds_EmptyInput(t *testing.T) {
	ensureVolcAssetTable(t)

	owned, err := FilterOwnedVolcAssetIds(1, 10, VolcResourceTypeAsset, nil)
	require.NoError(t, err)
	assert.Empty(t, owned)
}

// ---------------------------------------------------------------------------
// 客户视角列表
// ---------------------------------------------------------------------------

func TestGetVolcAssetsByUser(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "other"})

	// 不带类型筛：拿全部自己名下的
	rows, total, err := GetVolcAssetsByUser(1, "", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, 1, r.UserId, "客户视角不得混入别人的行")
	}

	// 带类型筛
	rows, total, err = GetVolcAssetsByUser(1, VolcResourceTypeAsset, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "a1", rows[0].ResourceId)

	// 分页：新的在前（id desc）
	rows, total, err = GetVolcAssetsByUser(1, "", 0, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "total 是总数而非本页条数")
	require.Len(t, rows, 1)
	assert.Equal(t, "g1", rows[0].ResourceId, "新的在前")

	rows, _, err = GetVolcAssetsByUser(1, "", 1, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "a1", rows[0].ResourceId)
}

// ---------------------------------------------------------------------------
// 超管视角列表
// ---------------------------------------------------------------------------

func TestGetVolcAssetsByFilter(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "asset-aaa", Name: "sunset clip", Status: "Active",
	})
	seedVolcAsset(t, &VolcAsset{
		UserId: 2, ChannelId: 11, ResourceType: VolcResourceTypeAssetGroup,
		ResourceId: "group-bbb", Name: "portraits", Status: "Processing",
	})
	seedVolcAsset(t, &VolcAsset{
		UserId: VolcAssetOrphanOwnerId, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "asset-ccc", Name: "orphan", Status: "Active",
	})

	cases := []struct {
		name   string
		filter VolcAssetFilter
		want   int64
	}{
		{"无筛选看到全部（含孤儿）", VolcAssetFilter{}, 3},
		{"按用户", VolcAssetFilter{UserId: 1}, 1},
		{"按渠道", VolcAssetFilter{ChannelId: 10}, 2},
		{"按资源类型", VolcAssetFilter{ResourceType: VolcResourceTypeAsset}, 2},
		{"按状态", VolcAssetFilter{Status: "Processing"}, 1},
		{"关键词命中 resource_id", VolcAssetFilter{Keyword: "asset-aaa"}, 1},
		{"关键词命中 name", VolcAssetFilter{Keyword: "portraits"}, 1},
		{"关键词跨列模糊", VolcAssetFilter{Keyword: "asset-"}, 2},
		{"组合筛选", VolcAssetFilter{UserId: 1, ChannelId: 10, Status: "Active"}, 1},
		{"组合筛选无交集", VolcAssetFilter{UserId: 1, ChannelId: 11}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, total, err := GetVolcAssetsByFilter(c.filter, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, c.want, total)
			assert.Len(t, rows, int(c.want))
		})
	}
}

// 资产 id 走精确匹配而非模糊:客户手上拿到的就是完整 id,精确查才不会被
// 同前缀的行淹没,也不会让 "asset-" 这种前缀把整页带出来。
func TestGetVolcAssetsByFilter_ResourceIdIsExact(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "asset-aaa",
	})
	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "asset-aaabbb",
	})

	rows, total, err := GetVolcAssetsByFilter(VolcAssetFilter{ResourceId: "asset-aaa"}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "精确匹配不得带出同前缀的行")
	require.Len(t, rows, 1)
	assert.Equal(t, "asset-aaa", rows[0].ResourceId)

	_, total, err = GetVolcAssetsByFilter(VolcAssetFilter{ResourceId: "asset-"}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total, "前缀不是完整 id,不该命中任何行")
}

// 时间范围按秒过滤,两端都是闭区间。
func TestGetVolcAssetsByFilter_TimeRange(t *testing.T) {
	ensureVolcAssetTable(t)

	for i, ts := range []int64{1000, 2000, 3000} {
		asset := &VolcAsset{
			UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
			ResourceId: "asset-" + string(rune('a'+i)), CreatedAt: ts,
		}
		seedVolcAsset(t, asset)
	}

	cases := []struct {
		name   string
		filter VolcAssetFilter
		want   int64
	}{
		{"只给起点", VolcAssetFilter{StartTimestamp: 2000}, 2},
		{"只给终点", VolcAssetFilter{EndTimestamp: 2000}, 2},
		{"闭区间两端都含", VolcAssetFilter{StartTimestamp: 1000, EndTimestamp: 3000}, 3},
		{"窄区间", VolcAssetFilter{StartTimestamp: 1500, EndTimestamp: 2500}, 1},
		{"区间外", VolcAssetFilter{StartTimestamp: 5000}, 0},
		{"零值不筛", VolcAssetFilter{}, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, total, err := GetVolcAssetsByFilter(c.filter, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, c.want, total)
		})
	}
}

// 客户视角与超管视角共用同一个谓词构造器 —— 两边支持的筛选项必须一致,
// 否则客户看到的筛选框会有一半点了没反应。
func TestGetVolcAssetsByUser_SharesFilterPredicates(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "mine", CreatedAt: 2000,
	})
	seedVolcAsset(t, &VolcAsset{
		UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "theirs", CreatedAt: 2000,
	})

	_, total, err := GetVolcAssetsByFilter(VolcAssetFilter{
		UserId: 1, ResourceId: "mine", StartTimestamp: 1000, EndTimestamp: 3000,
	}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// 即使资产 id 对得上，别人的行也绝不能被带出来。
	_, total, err = GetVolcAssetsByFilter(VolcAssetFilter{UserId: 1, ResourceId: "theirs"}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total, "归属过滤必须压过 id 精确匹配")
}

// 上限统计按渠道分别计数（配额各算各的），跨渠道总数另有一个函数供客户展示。
func TestCountVolcAssetsByUser_ScopedByChannel(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 11, ResourceType: VolcResourceTypeAsset, ResourceId: "a2"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a3"})

	onlyAssets := []string{VolcResourceTypeAsset}

	count, err := CountVolcAssetsByUser(1, 10, onlyAssets)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count, "按渠道计数不得把别的渠道算进来")

	count, err = CountVolcAssetsByUser(1, 10, []string{VolcResourceTypeAsset, VolcResourceTypeAssetGroup})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count, "把素材组算进上限时应一并统计")

	count, err = CountVolcAssetsByUserAllChannels(1, onlyAssets)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count, "客户看到的是跨渠道总数")
}

// 超管列表不带 user 过滤时能看到孤儿 —— 这正是「上游有、本地无记录」的素材
// 从「谁都看不见」变成「超管看得见且删得掉」的机制。
func TestGetVolcAssetsByFilter_OrphanVisibleToOneSide(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{
		UserId: VolcAssetOrphanOwnerId, ChannelId: 10,
		ResourceType: VolcResourceTypeAsset, ResourceId: "orphan-1",
	})

	_, adminTotal, err := GetVolcAssetsByFilter(VolcAssetFilter{}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, adminTotal, "超管必须看得见孤儿")

	_, userTotal, err := GetVolcAssetsByUser(1, "", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, userTotal, "客户看不到孤儿")
}

// num <= 0 时回退到 20，避免一次拉全表。
func TestPaginateVolcAssets_DefaultsPageSize(t *testing.T) {
	ensureVolcAssetTable(t)

	for i := 0; i < 25; i++ {
		seedVolcAsset(t, &VolcAsset{
			UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
			ResourceId: "a" + string(rune('a'+i%26)) + string(rune('0'+i/10)) + string(rune('0'+i%10)),
		})
	}

	rows, total, err := GetVolcAssetsByUser(1, "", 0, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 25, total)
	assert.Len(t, rows, 20, "num<=0 必须回退到默认页大小 20")
}

// ---------------------------------------------------------------------------
// 统计与遍历（对账用）
// ---------------------------------------------------------------------------

func TestCountVolcAssetsByChannelAndOwner(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a2"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: VolcAssetOrphanOwnerId, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "orphan"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 11, ResourceType: VolcResourceTypeAsset, ResourceId: "a3"})

	// 渠道全量（含孤儿、含各用户）。a1 / a2 / orphan 是 asset，a3 在渠道 11。
	for resourceType, want := range map[string]int64{
		VolcResourceTypeAsset:      3,
		VolcResourceTypeAssetGroup: 1,
	} {
		got, err := CountVolcAssetsByChannel(10, resourceType)
		require.NoError(t, err)
		assert.EqualValues(t, want, got, "channel 10 / %s", resourceType)
	}

	// 按归属：普通用户与孤儿的计数是分开的两件事，
	// 因为这两种情况在页面上的处置完全不同（客户自查 vs 超管清理）。
	userCount, err := CountVolcAssetsByChannelAndOwner(10, 1, VolcResourceTypeAsset)
	require.NoError(t, err)
	assert.EqualValues(t, 2, userCount)

	orphanCount, err := CountVolcAssetsByChannelAndOwner(10, VolcAssetOrphanOwnerId, VolcResourceTypeAsset)
	require.NoError(t, err)
	assert.EqualValues(t, 1, orphanCount)
}

// 同步要拿它和官方全量做差集，故不能分页 —— 分页会让未落到当前页的已记录
// 素材被误判成孤儿。
func TestListVolcAssetsByChannel_UnpaginatedIndexByResourceId(t *testing.T) {
	ensureVolcAssetTable(t)

	for i := 0; i < 30; i++ {
		seedVolcAsset(t, &VolcAsset{
			UserId: i%3 + 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
			ResourceId: "a" + string(rune('a'+i%26)) + string(rune('0'+i/10)) + string(rune('0'+i%10)),
		})
	}
	// 噪声：别的渠道 / 别的类型
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 11, ResourceType: VolcResourceTypeAsset, ResourceId: "noise-channel"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "noise-type"})

	index, err := ListVolcAssetsByChannel(10, VolcResourceTypeAsset)
	require.NoError(t, err)
	assert.Len(t, index, 30, "必须全量返回，不能分页")
	assert.NotContains(t, index, "noise-channel")
	assert.NotContains(t, index, "noise-type")

	for id, row := range index {
		assert.Equal(t, id, row.ResourceId)
		assert.Equal(t, 10, row.ChannelId)
		assert.Equal(t, VolcResourceTypeAsset, row.ResourceType)
	}
}

// ---------------------------------------------------------------------------
// 删除
// ---------------------------------------------------------------------------

func TestDeleteVolcAssetById(t *testing.T) {
	ensureVolcAssetTable(t)

	seed := seedVolcAsset(t, &VolcAsset{
		UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a1",
	})

	require.NoError(t, DeleteVolcAssetById(seed.Id))

	_, err := GetVolcAssetById(seed.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// 删不存在的行不报错（幂等）
	assert.NoError(t, DeleteVolcAssetById(seed.Id))
}

func TestGetVolcAssetById(t *testing.T) {
	ensureVolcAssetTable(t)

	seed := seedVolcAsset(t, &VolcAsset{
		UserId: 7, ChannelId: 10, ResourceType: VolcResourceTypeAsset,
		ResourceId: "a1", GroupId: "g1", Name: "n",
	})

	got, err := GetVolcAssetById(seed.Id)
	require.NoError(t, err)
	assert.Equal(t, "a1", got.ResourceId)
	assert.Equal(t, 7, got.UserId)
	assert.Equal(t, "g1", got.GroupId)
}

// 官方 DeleteAssetGroup 会批量删掉组内所有素材且不可逆，本地必须跟着级联，
// 否则那些行变成永远删不掉的记录 —— 重新删单个素材会被官方报「不存在」，
// 而页面还显示它们存在。
func TestDeleteVolcAssetsByGroupId_CascadesWithinChannelOnly(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a1", GroupId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a2", GroupId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: 2, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "a3", GroupId: "g1"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "other", GroupId: "g2"})
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "nogroup", GroupId: ""})
	// 另一个渠道的同名组：不同火山账号，不得被连坐删除
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 11, ResourceType: VolcResourceTypeAsset, ResourceId: "a4", GroupId: "g1"})
	// 组自身的记录（GroupId 为空）不应被这个级联删掉
	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAssetGroup, ResourceId: "g1"})

	affected, err := DeleteVolcAssetsByGroupId(10, "g1")
	require.NoError(t, err)
	assert.EqualValues(t, 3, affected, "本渠道本组的全部素材行都要删")

	remaining, total, err := GetVolcAssetsByFilter(VolcAssetFilter{ChannelId: 10}, 0, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	ids := map[string]bool{}
	for _, r := range remaining {
		ids[r.ResourceId] = true
	}
	assert.True(t, ids["other"], "别的组不得被连坐")
	assert.True(t, ids["nogroup"], "无组素材不得被连坐")
	assert.True(t, ids["g1"], "组记录自身保留，由调用方单独删")

	// 另一渠道的同名组完好
	_, otherTotal, err := GetVolcAssetsByFilter(VolcAssetFilter{ChannelId: 11}, 0, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 1, otherTotal, "另一个渠道的同名组不得被连坐")
}

// 空 groupId 直接返回 0 且不发查询 —— 否则 `group_id = ”` 会把所有无组素材
// 一扫而空。
func TestDeleteVolcAssetsByGroupId_EmptyGroupIdNoop(t *testing.T) {
	ensureVolcAssetTable(t)

	seedVolcAsset(t, &VolcAsset{UserId: 1, ChannelId: 10, ResourceType: VolcResourceTypeAsset, ResourceId: "nogroup", GroupId: ""})

	affected, err := DeleteVolcAssetsByGroupId(10, "")
	require.NoError(t, err)
	assert.EqualValues(t, 0, affected)

	_, total, err := GetVolcAssetsByFilter(VolcAssetFilter{ChannelId: 10}, 0, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "无组素材必须留下")
}
