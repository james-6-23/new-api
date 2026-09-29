package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// volc_assets 不在本包 TestMain 的自动迁移列表里（那张列表由无关的测试维护），
// 故本文件自行建表。idempotent，多个测试重复调用安全。
var volcAssetTableOnce sync.Once

func ensureVolcAssetTable(t *testing.T) {
	t.Helper()
	volcAssetTableOnce.Do(func() {
		if err := model.DB.AutoMigrate(&model.VolcAsset{}); err != nil {
			t.Fatalf("migrate volc_assets: %v", err)
		}
	})
	t.Cleanup(func() { model.DB.Exec("DELETE FROM volc_assets") })
}

func seedOwnedAsset(t *testing.T, userId, channelId int, resourceType, resourceId string) {
	t.Helper()
	require.NoError(t, model.RecordVolcAsset(&model.VolcAsset{
		UserId: userId, ChannelId: channelId,
		ResourceType: resourceType, ResourceId: resourceId,
	}))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := common.Marshal(v)
	require.NoError(t, err)
	return b
}

// ---------------------------------------------------------------------------
// 分支一：单资源 Action 的归属校验（转发前拒，不发上游）
// ---------------------------------------------------------------------------

func TestCheckSingleResourceOwnership(t *testing.T) {
	ensureVolcAssetTable(t)

	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "mine-asset")
	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAssetGroup, "mine-group")
	seedOwnedAsset(t, 2, 10, model.VolcResourceTypeAsset, "their-asset")
	seedOwnedAsset(t, 1, 11, model.VolcResourceTypeAsset, "other-channel-asset")

	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	t.Run("本人本渠道放行", func(t *testing.T) {
		for _, c := range []struct{ action, id string }{
			{"GetAsset", "mine-asset"},
			{"UpdateAsset", "mine-asset"},
			{"DeleteAsset", "mine-asset"},
			{"GetAssetGroup", "mine-group"},
			{"UpdateAssetGroup", "mine-group"},
			{"DeleteAssetGroup", "mine-group"},
		} {
			handled, err := CheckSingleResourceOwnership(owner, c.action,
				mustMarshal(t, map[string]any{"Id": c.id}))
			assert.NoError(t, err, "%s/%s 应放行", c.action, c.id)
			assert.True(t, handled, "%s 是单资源 Action，必须被处理", c.action)
		}
	})

	t.Run("不属于自己一律 ErrAssetNotOwned", func(t *testing.T) {
		for _, c := range []struct{ name, action, id string }{
			{"跨用户", "GetAsset", "their-asset"},
			{"跨用户删除", "DeleteAsset", "their-asset"},
			{"跨渠道同 id", "GetAsset", "other-channel-asset"},
			{"跨资源类型（asset 查 group 的 id）", "GetAsset", "mine-group"},
			{"跨资源类型（group 查 asset 的 id）", "GetAssetGroup", "mine-asset"},
			{"不存在的 id", "DeleteAsset", "no-such-asset"},
		} {
			handled, err := CheckSingleResourceOwnership(owner, c.action,
				mustMarshal(t, map[string]any{"Id": c.id}))
			assert.ErrorIs(t, err, ErrAssetNotOwned, c.name)
			assert.True(t, handled, c.name)
		}
	})

	// 空 id 与缺失 id 都归到「不拥有」：交给统一 404，
	// 而不是在这里替官方做参数校验（官方对缺参会给更准确的错误）。
	t.Run("空 id 判为不拥有", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"Id":""}`, `{"Id":123}`} {
			_, err := CheckSingleResourceOwnership(owner, "GetAsset", []byte(body))
			assert.ErrorIs(t, err, ErrAssetNotOwned, "body=%s", body)
		}
	})

	// 非单资源 Action 返回 (false, nil)，调用方无需处理。
	t.Run("非单资源 Action 不处理", func(t *testing.T) {
		for _, action := range []string{"CreateAsset", "ListAssets", "ListAssetGroups", "CreateAssetGroup"} {
			handled, err := CheckSingleResourceOwnership(owner, action, []byte(`{"Id":"mine-asset"}`))
			assert.NoError(t, err)
			assert.False(t, handled, "%s 不是单资源 Action", action)
		}
	})
}

// ---------------------------------------------------------------------------
// 分支二：CreateAsset 的 GroupId 校验（防止往别人的组塞素材）
// ---------------------------------------------------------------------------

func TestCheckCreateAssetGroupRef(t *testing.T) {
	ensureVolcAssetTable(t)

	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAssetGroup, "mine-group")
	seedOwnedAsset(t, 2, 10, model.VolcResourceTypeAssetGroup, "their-group")
	seedOwnedAsset(t, 1, 11, model.VolcResourceTypeAssetGroup, "other-channel-group")

	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	// 自己的组放行
	assert.NoError(t, CheckCreateAssetGroupRef(owner, "CreateAsset",
		mustMarshal(t, map[string]any{"GroupId": "mine-group", "Name": "n"})))

	// 别人的组、别的渠道的同名组、不存在的组 —— 一律拒。
	// 少了这一层，客户能往别人的素材组里塞素材，组的归属就形同虚设。
	for _, name := range []string{"their-group", "other-channel-group", "no-such-group"} {
		err := CheckCreateAssetGroupRef(owner, "CreateAsset",
			mustMarshal(t, map[string]any{"GroupId": name, "Name": "n"}))
		assert.ErrorIs(t, err, ErrAssetNotOwned, name)
	}

	// GroupId 缺失或为空时放过：官方会自己报缺参错误，站点不代劳参数校验。
	for _, body := range []string{`{}`, `{"GroupId":""}`, `not json`, ``} {
		assert.NoError(t, CheckCreateAssetGroupRef(owner, "CreateAsset", []byte(body)), "body=%s", body)
	}

	// 非 CreateAsset 一律不处理
	for _, action := range []string{"GetAsset", "ListAssets", "CreateAssetGroup", ""} {
		assert.NoError(t, CheckCreateAssetGroupRef(owner, action,
			mustMarshal(t, map[string]any{"GroupId": "their-group"})))
	}
}

// ---------------------------------------------------------------------------
// 分支三：视频提交的 asset:// 引用校验（隔离的闭环）
// ---------------------------------------------------------------------------

func TestCheckAssetReferences(t *testing.T) {
	ensureVolcAssetTable(t)

	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "mine-1")
	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "mine-2")
	seedOwnedAsset(t, 2, 10, model.VolcResourceTypeAsset, "theirs")
	seedOwnedAsset(t, 1, 11, model.VolcResourceTypeAsset, "other-channel")

	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	// 全部属于自己：无拒绝项
	rejected, err := CheckAssetReferences(owner, mustMarshal(t, map[string]any{
		"model": "doubao-seedance-1-0-pro-250528",
		"content": []any{
			map[string]any{"type": "text", "text": "animate this"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "asset://mine-1"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "asset://mine-2"}},
		},
	}))
	assert.NoError(t, err)
	assert.Empty(t, rejected)

	// 混入别人的 id：整单拒，并指出具体是哪个
	rejected, err = CheckAssetReferences(owner, mustMarshal(t, map[string]any{
		"content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "asset://mine-1"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "asset://theirs"}},
		},
	}))
	assert.NoError(t, err)
	assert.Equal(t, []string{"theirs"}, rejected,
		"必须整单拒并指出越权 id，而不是静默过滤掉别人的素材再提交")

	// 别的渠道的同 id：同样拒
	rejected, err = CheckAssetReferences(owner,
		mustMarshal(t, map[string]any{"content": []any{"asset://other-channel"}}))
	assert.NoError(t, err)
	assert.Equal(t, []string{"other-channel"}, rejected)

	// 完全不含 asset:// 引用（纯文生视频）：无拒绝项
	rejected, err = CheckAssetReferences(owner, mustMarshal(t, map[string]any{
		"model": "doubao-seedance-1-0-pro-250528",
		"content": []any{
			map[string]any{"type": "text", "text": "a cat surfing"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
		},
	}))
	assert.NoError(t, err)
	assert.Empty(t, rejected)

	// 非 JSON 请求体：解析不出引用，交给官方去报错
	rejected, err = CheckAssetReferences(owner, []byte(`not json`))
	assert.NoError(t, err)
	assert.Empty(t, rejected)

	// 同一越权 id 出现多次只报一次（ExtractAssetIds 已去重）
	rejected, err = CheckAssetReferences(owner,
		mustMarshal(t, map[string]any{"content": []any{"asset://theirs", "asset://theirs"}}))
	assert.NoError(t, err)
	assert.Equal(t, []string{"theirs"}, rejected)
}

// ---------------------------------------------------------------------------
// 分支四：List 响应的归属过滤
// ---------------------------------------------------------------------------

func TestFilterListResponseByOwner(t *testing.T) {
	ensureVolcAssetTable(t)

	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "mine-1")
	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "mine-2")
	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAssetGroup, "mine-group")

	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	t.Run("ListAssets 只留自己的条目", func(t *testing.T) {
		resp := []byte(`{"ResponseMetadata":{},"Result":{"Items":[
			{"Id":"mine-1","Name":"a","Status":"Active"},
			{"Id":"theirs-1","Name":"b","Status":"Active"},
			{"Id":"mine-2","Name":"c","Status":"Processing"},
			{"Id":"theirs-2","Name":"d","Status":"Active"}
		],"TotalCount":4}}`)

		out, changed, err := FilterListResponseByOwner(owner, "ListAssets", resp)
		require.NoError(t, err)
		assert.True(t, changed)

		var parsed map[string]any
		require.NoError(t, common.Unmarshal(out, &parsed))
		result := parsed["Result"].(map[string]any)
		items := result["Items"].([]any)
		require.Len(t, items, 2, "别人的条目必须被过滤掉")
		for _, raw := range items {
			id := raw.(map[string]any)["Id"].(string)
			assert.Contains(t, []string{"mine-1", "mine-2"}, id)
		}
		assert.EqualValues(t, 2, result["TotalCount"],
			"TotalCount 必须一起改小，否则客户以为站点丢数据")
	})

	// ListAssets 的条目是素材，不该拿素材组归属去匹配；反过来同理。
	t.Run("资源类型不串门", func(t *testing.T) {
		resp := []byte(`{"Result":{"Items":[{"Id":"mine-group"},{"Id":"mine-1"}]}}`)
		out, _, err := FilterListResponseByOwner(owner, "ListAssets", resp)
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, common.Unmarshal(out, &parsed))
		items := parsed["Result"].(map[string]any)["Items"].([]any)
		require.Len(t, items, 1)
		assert.Equal(t, "mine-1", items[0].(map[string]any)["Id"])

		out, _, err = FilterListResponseByOwner(owner, "ListAssetGroups", resp)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(out, &parsed))
		items = parsed["Result"].(map[string]any)["Items"].([]any)
		require.Len(t, items, 1)
		assert.Equal(t, "mine-group", items[0].(map[string]any)["Id"])
	})

	t.Run("非 List Action 原样返回", func(t *testing.T) {
		resp := []byte(`{"Result":{"Items":[{"Id":"theirs-1"}]}}`)
		for _, action := range []string{"GetAsset", "CreateAsset", "CreateAssetGroup", ""} {
			out, changed, err := FilterListResponseByOwner(owner, action, resp)
			assert.NoError(t, err)
			assert.False(t, changed, action)
			assert.Equal(t, resp, out, action)
		}
	})

	t.Run("空 Items 也要改写 TotalCount", func(t *testing.T) {
		resp := []byte(`{"Result":{"Items":[],"TotalCount":7}}`)
		out, changed, err := FilterListResponseByOwner(owner, "ListAssets", resp)
		require.NoError(t, err)
		assert.True(t, changed)

		var parsed map[string]any
		require.NoError(t, common.Unmarshal(out, &parsed))
		assert.EqualValues(t, 0, parsed["Result"].(map[string]any)["TotalCount"],
			"上游全量 7 条里没有一条属于我，客户看到的必须是 0")
	})

	t.Run("响应解析不了时原样返回", func(t *testing.T) {
		for _, resp := range []string{`not json`, ``, `{"ResponseMetadata":{}}`} {
			out, changed, err := FilterListResponseByOwner(owner, "ListAssets", []byte(resp))
			assert.NoError(t, err)
			assert.False(t, changed, "resp=%q", resp)
			assert.Equal(t, resp, string(out))
		}
	})

	// 过滤同时顺手刷新留下条目的快照：List 响应本来就带 name/status，
	// 客户轮询越勤，管理页面看到的状态越新。
	t.Run("顺手刷新留下条目的快照", func(t *testing.T) {
		resp := []byte(`{"Result":{"Items":[
			{"Id":"mine-1","Name":"新名字","Status":"Active","GroupId":"g-new","AssetType":"Video"}
		]}}`)
		_, _, err := FilterListResponseByOwner(owner, "ListAssets", resp)
		require.NoError(t, err)

		var row model.VolcAsset
		require.NoError(t, model.DB.Where("resource_id = ?", "mine-1").First(&row).Error)
		assert.Equal(t, "新名字", row.Name)
		assert.Equal(t, "Active", row.Status)
		assert.Equal(t, "g-new", row.GroupId)
		assert.Equal(t, "Video", row.AssetType)
		assert.Equal(t, 1, row.UserId, "刷新不得改归属")
	})
}

// ---------------------------------------------------------------------------
// 写入口：RecordCreatedResource
// ---------------------------------------------------------------------------

func TestRecordCreatedResource(t *testing.T) {
	ensureVolcAssetTable(t)

	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	t.Run("CreateAsset 落库，快照取自请求体", func(t *testing.T) {
		asset, err := RecordCreatedResource(owner, "CreateAsset",
			[]byte(`{"GroupId":"g1","Name":"my clip","AssetType":"Video"}`),
			[]byte(`{"ResponseMetadata":{},"Result":{"Id":"asset-new"}}`))
		require.NoError(t, err)
		require.NotNil(t, asset)
		assert.Equal(t, "asset-new", asset.ResourceId)
		assert.Equal(t, model.VolcResourceTypeAsset, asset.ResourceType)
		assert.Equal(t, "g1", asset.GroupId)
		assert.Equal(t, "my clip", asset.Name)
		assert.Equal(t, "Video", asset.AssetType)

		// 立刻可读，且只对该用户可见
		owned, err := model.IsVolcAssetOwnedBy(1, 10, model.VolcResourceTypeAsset, "asset-new")
		require.NoError(t, err)
		assert.True(t, owned)
		owned, err = model.IsVolcAssetOwnedBy(2, 10, model.VolcResourceTypeAsset, "asset-new")
		require.NoError(t, err)
		assert.False(t, owned)
	})

	t.Run("CreateAssetGroup 落库", func(t *testing.T) {
		asset, err := RecordCreatedResource(owner, "CreateAssetGroup",
			[]byte(`{"Name":"ag"}`),
			[]byte(`{"Result":{"Id":"group-new"}}`))
		require.NoError(t, err)
		require.NotNil(t, asset)
		assert.Equal(t, model.VolcResourceTypeAssetGroup, asset.ResourceType)
		assert.Equal(t, "group-new", asset.ResourceId)
	})

	// CreateVisualValidateSession 返回的是 30 分钟有效的临时 BytedToken，
	// 不是资源 —— 落库只会留下一行永远查不到的垃圾。
	t.Run("临时 BytedToken 不落库", func(t *testing.T) {
		asset, err := RecordCreatedResource(owner, "CreateVisualValidateSession",
			nil, []byte(`{"Result":{"BytedToken":"tok","GroupId":"g"}}`))
		require.NoError(t, err)
		assert.Nil(t, asset)
	})

	t.Run("非创建类 Action 不落库", func(t *testing.T) {
		for _, action := range []string{"GetAsset", "ListAssets", "DeleteAsset", ""} {
			asset, err := RecordCreatedResource(owner, action, nil, []byte(`{"Result":{"Id":"x"}}`))
			assert.NoError(t, err)
			assert.Nil(t, asset, action)
		}
	})

	t.Run("响应无 Id 不落库", func(t *testing.T) {
		asset, err := RecordCreatedResource(owner, "CreateAsset", []byte(`{}`), []byte(`{"Result":{}}`))
		assert.NoError(t, err)
		assert.Nil(t, asset)
	})
}

// ---------------------------------------------------------------------------
// 写入口：RefreshSnapshotFromResponse
// ---------------------------------------------------------------------------

func TestRefreshSnapshotFromResponse(t *testing.T) {
	ensureVolcAssetTable(t)

	seedOwnedAsset(t, 1, 10, model.VolcResourceTypeAsset, "a1")
	owner := VolcAssetOwner{UserId: 1, ChannelId: 10}

	RefreshSnapshotFromResponse(owner, "GetAsset", []byte(`{"Result":{
		"Id":"a1","GroupId":"g1","Name":"refreshed","AssetType":"Image","Status":"Active"}}`))

	var row model.VolcAsset
	require.NoError(t, model.DB.Where("resource_id = ?", "a1").First(&row).Error)
	assert.Equal(t, "refreshed", row.Name)
	assert.Equal(t, "Active", row.Status)
	assert.Equal(t, "g1", row.GroupId)
	assert.Equal(t, 1, row.UserId, "刷新不得改归属")
	assert.Equal(t, 10, row.ChannelId)

	// 只有 Get 类需要刷新；List 走 FilterListResponseByOwner，其余不碰。
	for _, action := range []string{"ListAssets", "CreateAsset", "UpdateAsset", "DeleteAsset", "GetVisualValidateResult", ""} {
		RefreshSnapshotFromResponse(owner, action, []byte(`{"Result":{"Id":"zzz","Name":"不该写"}}`))
		var count int64
		require.NoError(t, model.DB.Model(&model.VolcAsset{}).Where("resource_id = ?", "zzz").Count(&count).Error)
		assert.Zero(t, count, "%s 不该刷新快照", action)
	}

	// 响应里没有 Id 时不动任何行
	RefreshSnapshotFromResponse(owner, "GetAsset", []byte(`{"Result":{"Name":"no id"}}`))
	require.NoError(t, model.DB.Where("resource_id = ?", "a1").First(&row).Error)
	assert.Equal(t, "refreshed", row.Name, "无 Id 的响应不得写入")
}
