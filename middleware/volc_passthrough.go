package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// VolcPassthroughPathKey 存放剥离 /volc 前缀后的官方原始路径。
const VolcPassthroughPathKey = "volc_passthrough_path"

// VolcPassthroughBilledKey 标记本次请求是否走计费的任务链路。
const VolcPassthroughBilledKey = "volc_passthrough_billed"

// volcGenerationsPath 官方视频生成任务路径,唯一计费入口。
const volcGenerationsPath = "/api/v3/contents/generations/tasks"

// VolcPassthroughDistribute 为字节火山透传渠道选定渠道。
//
// 透传路径分两类,选渠道方式不同:
//
//	视频生成提交  请求体里有官方 model 字段 → 直接复用既有 Distribute(),
//	              走与其他渠道完全一致的分组/模型/优先级/权重选路与计费。
//	其余全部接口  没有 model 字段(112 个控制面 Action、Files 上传、任务查询/删除),
//	              模型索引用不上,按渠道类型在分组内选;任务相关路径进一步按 task_id
//	              反查提交时的渠道。
//
// 任务查询必须回到提交渠道:不同渠道的密钥与项目互不可见,查错渠道会 404,
// 任务卡在 in_progress 且额度一直被预扣(教训来自 doubao 适配器的同源约束)。
func VolcPassthroughDistribute() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := normalizeVolcPassthroughPath(c.Request.URL.Path)
		c.Set(VolcPassthroughPathKey, path)

		if c.Request.Method == http.MethodPost && strings.TrimRight(path, "/") == volcGenerationsPath {
			c.Set(VolcPassthroughBilledKey, true)
			c.Set("relay_mode", relayconstant.RelayModeVideoSubmit)
			// Distribute 内部会 c.Next(),后续链路在其中执行。
			Distribute()(c)
			return
		}

		channel, err := resolveVolcPassthroughChannel(c, path)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusServiceUnavailable, err.Error(), types.ErrorCodeGetChannelFailed)
			return
		}
		if setupErr := SetupContextForSelectedChannel(c, channel, ""); setupErr != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, setupErr.Error(), types.ErrorCodeGetChannelFailed)
			return
		}
		c.Next()
	}
}

// normalizeVolcPassthroughPath 把站点路径还原成官方原始路径。
//
// 数据面直接挂在站点根上(/api/v3/... 与官方逐字一致),无需剥离;
// 控制面因官方挂在 host 根、而站点根被前端占用,保留 /volc 前缀,
// 此处剥掉后即得官方的根路径 "/"。/volc 同样兼容数据面写法。
func normalizeVolcPassthroughPath(raw string) string {
	path := raw
	if path == "/volc" {
		return "/"
	}
	if strings.HasPrefix(path, "/volc/") {
		path = strings.TrimPrefix(path, "/volc")
	}
	if path == "" {
		return "/"
	}
	return path
}

// resolveVolcPassthroughChannel 为不带模型名的透传请求定位渠道。
func resolveVolcPassthroughChannel(c *gin.Context, path string) (*model.Channel, error) {
	if taskID := extractVolcTaskID(path); taskID != "" {
		task, exist, err := model.GetByOnlyTaskId(taskID)
		if err == nil && exist && task != nil && task.ChannelId > 0 {
			ch, err := model.CacheGetChannel(task.ChannelId)
			if err != nil {
				return nil, fmt.Errorf("任务 %s 的提交渠道 #%d 不可用: %w", taskID, task.ChannelId, err)
			}
			if ch.Status != common.ChannelStatusEnabled {
				return nil, fmt.Errorf("任务 %s 的提交渠道 #%d 已被禁用", taskID, task.ChannelId)
			}
			return ch, nil
		}
		// 查不到本地任务记录时不报错:客户可能查询的是直连官方创建的任务,
		// 继续按渠道类型选路,由官方返回权威结果。
	}

	for _, group := range candidateGroups(c) {
		ch, err := model.GetRandomSatisfiedChannelByType(group, constant.ChannelTypeVolcPassthrough)
		if err != nil {
			return nil, fmt.Errorf("获取分组 %s 下的字节火山透传渠道失败: %w", group, err)
		}
		if ch != nil {
			return ch, nil
		}
	}
	return nil, fmt.Errorf("分组 %s 下没有可用的字节火山透传渠道",
		common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
}

// candidateGroups 返回本次请求可用于选渠道的分组,按优先顺序。
//
// usingGroup 可能是字面分组名,也可能是 auto —— auto 不是真实分组,
// 直接拿去查渠道永远查不到。多分组令牌同理:首个分组没有透传渠道时
// 应继续尝试后续分组,与模型路径的跨分组重试行为保持一致。
func candidateGroups(c *gin.Context) []string {
	using := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if using == "auto" {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		if groups := service.GetUserAutoGroup(userGroup); len(groups) > 0 {
			return groups
		}
		if userGroup != "" {
			return []string{userGroup}
		}
		return nil
	}
	if groups, ok := common.GetContextKeyType[[]string](c, constant.ContextKeyTokenGroups); ok && len(groups) > 0 {
		return groups
	}
	if using != "" {
		return []string{using}
	}
	return nil
}

// extractVolcTaskID 从任务路径尾部取出 task id。
// 匹配 /api/v3/contents/generations/tasks/{id}(GET 查询与 DELETE 删除同形)。
func extractVolcTaskID(path string) string {
	prefix := volcGenerationsPath + "/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// VolcAssetReferenceGuard 校验视频提交里全部 asset:// 引用的归属。
//
// 这是整套资产隔离的闭环:前面三类校验(单资源 Action、CreateAsset 的 GroupId、
// List 过滤)都只管素材库接口,少了这一步,客户猜到别人的 asset id 就能直接拿去
// 生成视频,前三类校验全部形同虚设。
//
// 必须挂在 VolcPassthroughDistribute 之后:归属校验要用 channel_id,而渠道是
// Distribute 选出来的。Distribute 内部会 c.Next(),故链上后一个 handler 拿到的
// 已是选定渠道后的上下文。
func VolcAssetReferenceGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !c.GetBool(VolcPassthroughBilledKey) {
			c.Next()
			return
		}
		body, err := readVolcRequestBody(c)
		if err != nil {
			abortWithVolcAssetError(c, http.StatusBadRequest, "read_request_body_failed", err.Error())
			return
		}
		owner := service.VolcAssetOwner{
			UserId:    common.GetContextKeyInt(c, constant.ContextKeyUserId),
			ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		}
		rejected, err := service.CheckAssetReferences(owner, body)
		if err != nil {
			// 查不清归属时放行等于没有隔离,故宁可失败也不放过。
			abortWithVolcAssetError(c, http.StatusInternalServerError,
				"asset_ownership_check_failed", err.Error())
			return
		}
		if len(rejected) > 0 {
			// 整单拒而不是「过滤掉不属于你的素材再提交」—— 后者会静默改变客户的
			// 生成意图。措辞与素材库接口一致,不区分「不存在」与「不属于你」。
			abortWithVolcAssetError(c, http.StatusNotFound, "resource_not_found",
				"素材不存在或不属于当前账号: "+strings.Join(rejected, ", "))
			return
		}
		c.Next()
	}
}

// readVolcRequestBody 读取请求体且不影响后续读取(BodyStorage 每次都从头 seek)。
func readVolcRequestBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

// abortVolcAssetError 用站点自己的错误形状回写,不伪装成官方错误 ——
// 客户能立刻分清是站点挡的还是火山挡的。
func abortWithVolcAssetError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
		},
	})
	c.Abort()
}
