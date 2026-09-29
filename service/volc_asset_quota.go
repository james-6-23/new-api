package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ErrAssetLimitExceeded 表示当前账号持有的素材数已达站点分配的上限。
var ErrAssetLimitExceeded = errors.New("volc asset limit exceeded")

// ResolveVolcAssetLimit 算出某用户实际适用的素材条数上限。
//
// 优先级:用户个人覆盖值 > 全局默认。返回 0 表示不限 —— 全局默认填 0、
// 或个人被显式设成「不限」,两种都落到 0,调用方只需判一次。
func ResolveVolcAssetLimit(userId int) int {
	setting := operation_setting.GetVolcAssetSetting()
	limit := setting.DefaultAssetLimit

	user, err := model.GetUserById(userId, true)
	if err == nil && user != nil {
		switch override := user.GetSetting().VolcAssetLimit; {
		case override == dto.VolcAssetLimitUnlimited:
			return 0
		case override > 0:
			limit = override
		}
	}
	if limit < 0 {
		return 0
	}
	return limit
}

// CountVolcAssetsForLimit 统计计入上限的持有量。
func CountVolcAssetsForLimit(owner VolcAssetOwner) (int64, error) {
	types := []string{model.VolcResourceTypeAsset}
	if operation_setting.GetVolcAssetSetting().CountAssetGroups {
		types = append(types, model.VolcResourceTypeAssetGroup)
	}
	return model.CountVolcAssetsByUser(owner.UserId, owner.ChannelId, types)
}

// CheckVolcAssetLimit 在创建类 Action 转发前校验上限。
//
// 只拦创建:删除与查询不增加持有量。必须在转发之前拦 —— 转发后再拦,素材已经
// 建在火山侧并占着配额,本地却因为超限不记账,直接变成孤儿。
//
// 关闭开关时只展示不拦截,默认如此:升级后已超额的老客户不该突然创建失败。
func CheckVolcAssetLimit(owner VolcAssetOwner, action string) error {
	setting := operation_setting.GetVolcAssetSetting()
	if !setting.Enabled {
		return nil
	}
	if !isVolcAssetCreateAction(action, setting.CountAssetGroups) {
		return nil
	}
	limit := ResolveVolcAssetLimit(owner.UserId)
	if limit <= 0 {
		return nil
	}
	used, err := CountVolcAssetsForLimit(owner)
	if err != nil {
		// 数不清就不放行:放行等于上限形同虚设,而创建失败是可重试的。
		return fmt.Errorf("统计素材持有量失败: %w", err)
	}
	if used >= int64(limit) {
		return fmt.Errorf("%w: 已持有 %d 条,上限 %d 条", ErrAssetLimitExceeded, used, limit)
	}
	return nil
}

func isVolcAssetCreateAction(action string, countGroups bool) bool {
	switch action {
	case "CreateAsset":
		return true
	case "CreateAssetGroup":
		return countGroups
	default:
		return false
	}
}
