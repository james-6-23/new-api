package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// GetRandomSatisfiedChannelByType 在指定分组下按渠道类型挑选一个可用渠道,不要求模型名。
//
// 既有的 GetRandomSatisfiedChannel 以「分组 + 模型」为键从 abilities 索引选渠道,
// 但字节火山透传渠道有大量不带模型名的接口(112 个控制面 Action、Files 上传、
// 任务查询/删除),这些请求体里根本没有 model 字段,无法走模型索引。
//
// 选择规则与模型路径保持一致:先按优先级取最高档,档内按权重随机,
// 让管理员配置的优先级/权重对透传渠道同样生效。
func GetRandomSatisfiedChannelByType(group string, channelType int) (*Channel, error) {
	candidates, err := enabledChannelsByType(group, channelType)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GetPriority() > candidates[j].GetPriority()
	})
	topPriority := candidates[0].GetPriority()
	top := make([]*Channel, 0, len(candidates))
	for _, ch := range candidates {
		if ch.GetPriority() != topPriority {
			break
		}
		top = append(top, ch)
	}

	// 权重统一 +10,保证权重全为 0 时退化为等概率随机而非除零。
	weightSum := 0
	for _, ch := range top {
		weightSum += ch.GetWeight() + 10
	}
	pick := common.GetRandomInt(weightSum)
	for _, ch := range top {
		pick -= ch.GetWeight() + 10
		if pick <= 0 {
			return ch, nil
		}
	}
	return top[0], nil
}

// enabledChannelsByType 取出分组内指定类型的已启用渠道。
// 内存缓存开启时走缓存,否则回落数据库,与 GetRandomSatisfiedChannel 的两态行为一致。
func enabledChannelsByType(group string, channelType int) ([]*Channel, error) {
	if !common.MemoryCacheEnabled {
		var channels []*Channel
		// group 列在三种数据库里的引号写法不同,必须用 commonGroupCol。
		// 分组字段是逗号分隔的多值列,用 LIKE 匹配后在 Go 侧精确复核,
		// 避免 "vip" 命中 "vip-plus"(三库通用,不依赖任何数据库特有函数)。
		err := DB.Where("type = ? and status = ? and "+commonGroupCol+" LIKE ?",
			channelType, common.ChannelStatusEnabled, "%"+group+"%").Find(&channels).Error
		if err != nil {
			return nil, err
		}
		return filterChannelsByGroup(channels, group), nil
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channelsIDM == nil {
		return nil, fmt.Errorf("渠道缓存尚未初始化")
	}
	result := make([]*Channel, 0, 4)
	for _, ch := range channelsIDM {
		if ch == nil || ch.Type != channelType || ch.Status != common.ChannelStatusEnabled {
			continue
		}
		if channelInGroup(ch, group) {
			result = append(result, ch)
		}
	}
	return result, nil
}

func filterChannelsByGroup(channels []*Channel, group string) []*Channel {
	result := make([]*Channel, 0, len(channels))
	for _, ch := range channels {
		if channelInGroup(ch, group) {
			result = append(result, ch)
		}
	}
	return result
}

func channelInGroup(ch *Channel, group string) bool {
	for _, g := range strings.Split(ch.Group, ",") {
		if strings.TrimSpace(g) == group {
			return true
		}
	}
	return false
}
