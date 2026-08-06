package sora

import (
	"github.com/QuantumNous/new-api/relay/channel/task/seedance"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// Seedance 2.0 的识别、检测与计费统一收敛到共享包 relay/channel/task/seedance
// (单一事实来源,与 doubao 适配器共用同一套精确矩阵与 video_pricing 倍率)。
// 本文件仅保留 sora 适配器需要的、语义与旧接口一致的薄封装。

// IsSeedance2Model 判定是否为 Seedance 2.0 模型(委托共享包)。
func IsSeedance2Model(name string) bool {
	return seedance.IsSeedance2(name)
}

// estimateSeedance2Ratios 计算 Seedance 2.0 的 OtherRatios 并写入展示快照。
func estimateSeedance2Ratios(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	return seedance.EstimateBilling(c, info)
}

// HasImageInRequest 检测请求是否含图片输入(委托共享包),
// 用于把「文生视频」动作纠正为「图生视频」。
func HasImageInRequest(c *gin.Context, req *relaycommon.TaskSubmitReq) bool {
	return seedance.HasImageInput(c, req)
}
