package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/volcsign"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcpassthrough/assetiso"
)

// 超管侧的素材库运维:对账额度、同步孤儿、删除资产。
//
// 这些操作没有客户请求做载体,拿不到透传链路上的渠道上下文,故必须自己按渠道
// 配置签名调官方控制面 —— 与 controller/relay_passthrough.go 走的是同一套
// AK/SK 签名,只是凭据来源从 gin.Context 换成了 model.Channel。

const (
	// volcAssetAPIVersion 控制面 OpenAPI 版本,与透传链路一致。
	volcAssetAPIVersion = "2024-01-01"
	// volcAssetSignService 签名 Service 名。
	volcAssetSignService = "ark"
	// volcAssetListPageSize 遍历时每页条数,官方上限 100。
	volcAssetListPageSize = 100
	// volcAssetMaxPages 遍历页数上限,防止官方游标异常时无限翻页。
	// 100 页 × 100 条 = 1 万条,远超单渠道素材库的现实规模。
	volcAssetMaxPages = 100
)

// VolcAssetAdminClient 按渠道配置直连官方控制面。
type VolcAssetAdminClient struct {
	endpoint   string
	region     string
	accessKey  string
	secretKey  string
	proxy      string
	project    string
	channelId  int
	quotaLimit int
}

// NewVolcAssetAdminClient 从渠道配置构造客户端。
//
// AK/SK 缺失即报错而不静默跳过:静默跳过会让额度页显示「上游 0 条」,
// 超管会据此把本地全部记账当成孤儿清掉。
func NewVolcAssetAdminClient(channel *model.Channel) (*VolcAssetAdminClient, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	other := channel.GetOtherSettings()
	if other.BytePlusAccessKey == "" || other.BytePlusSecretKey == "" {
		return nil, fmt.Errorf("渠道 #%d 未配置 AK/SK,无法查询官方素材库", channel.Id)
	}
	baseURL := channel.GetBaseURL()
	_, project, _ := other.ResolveBytePlusAsset()
	if other.BytePlusProjectName != "" {
		project = other.BytePlusProjectName
	}
	return &VolcAssetAdminClient{
		endpoint:   other.ResolveVolcOpenAPIEndpoint(baseURL),
		region:     other.ResolveVolcSignRegion(baseURL),
		accessKey:  other.BytePlusAccessKey,
		secretKey:  other.BytePlusSecretKey,
		proxy:      channel.GetSetting().Proxy,
		project:    project,
		channelId:  channel.Id,
		quotaLimit: other.VolcAssetQuotaLimit,
	}, nil
}

// ChannelId 返回客户端绑定的渠道 id。
func (c *VolcAssetAdminClient) ChannelId() int { return c.channelId }

// QuotaLimit 返回管理员手填的资产条数上限,0 表示未知/不限。
// 官方没有查限额的接口,只能由管理员在渠道配置里填。
func (c *VolcAssetAdminClient) QuotaLimit() int { return c.quotaLimit }

// doAction 发一次签名后的控制面请求并返回响应体。
//
// 官方把业务错误放在 HTTP 200 的 ResponseMetadata.Error 里,故状态码与信封
// 两处都要判 —— 只看状态码会把「查询失败」读成「没有素材」。
func (c *VolcAssetAdminClient) doAction(ctx context.Context, action string, payload map[string]any) ([]byte, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/?Action=%s&Version=%s", strings.TrimRight(c.endpoint, "/"), action, volcAssetAPIVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if err := volcsign.SignRequest(req, body, c.accessKey, c.secretKey, c.region, volcAssetSignService); err != nil {
		return nil, fmt.Errorf("sign %s failed: %w", action, err)
	}

	client, err := GetHttpClientWithProxy(c.proxy)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", action, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response failed: %w", action, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if envErr := assetiso.ParseEnvelopeError(respBody); envErr != nil {
			return nil, fmt.Errorf("%s failed (%d): %w", action, resp.StatusCode, envErr)
		}
		return nil, fmt.Errorf("%s failed (%d): %s", action, resp.StatusCode, string(respBody))
	}
	if envErr := assetiso.ParseEnvelopeError(respBody); envErr != nil {
		return nil, fmt.Errorf("%s failed: %w", action, envErr)
	}
	return respBody, nil
}

// listAction 把资源类型映射到官方 List Action。
func listAction(resourceType string) (string, error) {
	switch resourceType {
	case model.VolcResourceTypeAsset:
		return "ListAssets", nil
	case model.VolcResourceTypeAssetGroup:
		return "ListAssetGroups", nil
	default:
		return "", fmt.Errorf("unknown volc resource type: %s", resourceType)
	}
}

// deleteAction 把资源类型映射到官方 Delete Action。
func deleteAction(resourceType string) (string, error) {
	switch resourceType {
	case model.VolcResourceTypeAsset:
		return "DeleteAsset", nil
	case model.VolcResourceTypeAssetGroup:
		return "DeleteAssetGroup", nil
	default:
		return "", fmt.Errorf("unknown volc resource type: %s", resourceType)
	}
}

// CountUpstreamAssets 取官方侧某类型资源的权威总数。
//
// 必须逐个 GroupType 查再相加:官方把 Filter.GroupType 定为必选,AIGC 与
// LivenessFace 是两套独立命名空间,查一类拿不到另一类。
//
// 走页码分页而非 NextToken:TotalCount 只在页码模式下返回,而额度对账要的
// 正是这个含孤儿的权威总数。PageSize 取 1 —— 只要计数,不要条目。
func (c *VolcAssetAdminClient) CountUpstreamAssets(ctx context.Context, resourceType string) (int, error) {
	action, err := listAction(resourceType)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, groupType := range assetiso.AssetGroupTypes {
		respBody, err := c.doAction(ctx, action, map[string]any{
			"Filter":      map[string]any{"GroupType": groupType},
			"PageNumber":  1,
			"PageSize":    1,
			"ProjectName": c.project,
		})
		if err != nil {
			return 0, err
		}
		count, ok := assetiso.ParseTotalCount(respBody)
		if !ok {
			// 官方没返回 TotalCount 说明它没按页码模式回答,此时把 0 当成
			// 「没有素材」会把本地全部记账误判成孤儿,故直接报错。
			return 0, fmt.Errorf("%s 未返回 TotalCount(GroupType=%s),无法对账", action, groupType)
		}
		total += count
	}
	return total, nil
}

// ListUpstreamAssets 遍历官方侧某类型的全部资源。
//
// 用 NextToken 分页而非页码:官方对页码模式有 PageSize*PageNumber 深翻页限制
// （素材 20000 / 素材组 10000）,而同步要的是全量,超限后页码模式直接取不到数据。
func (c *VolcAssetAdminClient) ListUpstreamAssets(ctx context.Context, resourceType string) ([]assetiso.ListedResource, error) {
	action, err := listAction(resourceType)
	if err != nil {
		return nil, err
	}
	var out []assetiso.ListedResource
	for _, groupType := range assetiso.AssetGroupTypes {
		nextToken := ""
		for page := 0; page < volcAssetMaxPages; page++ {
			payload := map[string]any{
				"Filter":      map[string]any{"GroupType": groupType},
				"MaxResults":  volcAssetListPageSize,
				"ProjectName": c.project,
			}
			if nextToken != "" {
				payload["NextToken"] = nextToken
			}
			respBody, err := c.doAction(ctx, action, payload)
			if err != nil {
				return nil, err
			}
			out = append(out, assetiso.ParseListedResources(respBody)...)
			nextToken = assetiso.ParseNextToken(respBody)
			if nextToken == "" {
				break
			}
		}
	}
	return out, nil
}

// DeleteUpstreamAsset 调官方删除单个资源。
//
// 资产组的删除会连带删掉组内全部素材且不可逆,故调用方在成功后必须一并清掉
// 本地的组内记账行,否则那些行永远删不掉(再删官方会报「不存在」)。
func (c *VolcAssetAdminClient) DeleteUpstreamAsset(ctx context.Context, resourceType, resourceId string) error {
	action, err := deleteAction(resourceType)
	if err != nil {
		return err
	}
	if resourceId == "" {
		return fmt.Errorf("resource id is empty")
	}
	_, err = c.doAction(ctx, action, map[string]any{
		"Id":          resourceId,
		"ProjectName": c.project,
	})
	return err
}
