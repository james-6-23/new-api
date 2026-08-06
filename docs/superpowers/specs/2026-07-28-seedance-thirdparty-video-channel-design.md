# 第三方 Seedance 视频渠道接入设计(与现有 seedance 隔离)

日期:2026-07-28
范围:新增一个独立渠道类型,接入第三方 Seedance 上游 `https://model.service-inference.ai`,与现有两条 Seedance 链路(`doubao` 渠道 45/54、`sora` 渠道 55/1)在 **wire 协议层完全隔离**,仅共享 Seedance 2.0 计费真相源。

上游接口文档:`接口文档/第三方seedance接口文档/sd2_real.md`(260128 族)、`sd_real_max.md`(hc 族)。

---

## Context(为什么做这件事)

现有 Seedance 2.0 有两条上游链路,都按渠道类型路由,并共用计费包 `relay/channel/task/seedance`(`pricing.go` 二维矩阵 + 单一 `video_pricing` 倍率,`billing.go` 提供 `IsSeedance2`/`EstimateBilling`/视频输入与分辨率探测):

- `doubao` 适配器:渠道 45/54,火山 `/api/v3/contents/generations/tasks`,响应 `{id}`。
- `sora` 适配器:渠道 55/1,OpenAI `/v1/videos`。

第三方 `model.service-inference.ai` 是**另一套 wire 协议**,不能塞进上述任一适配器而不互相污染:

- 提交:`POST /v1/video/generate`,响应嵌套 `{task:{id,status,...}}`。
- 查询:`GET /v1/video/tasks/{id}`,响应 `{task:{status,outputs:[url],usage:{completion_tokens,total_tokens},last_frame_url,error}}`。
- 状态值:`pending / processing / completed / failed`(与 doubao 的 `succeeded/expired` 不同)。
- 鉴权:`Authorization: Bearer <key>`(无 AK/SK 签名)。
- 素材库:两套接口(见 §3),需先上传拿 `asset://<id>` 再提交。
- 模型:客户端用原名 `dreamina-seedance-2-0-260128 / -fast-260128 / -mini-260615`;上游按变体可能要求 `-hc`(高一致)/`-ep` 等后缀名,由渠道模型映射在反代时补上。

**目标产出**:一个独立渠道类型 + 独立适配器包,speak 第三方协议;计费复用共享 `seedance` 包,不重复造矩阵;素材预上传按模型自动选流程;两个前端主题可配置该渠道。

---

## 隔离策略

**隔离点 = 新渠道类型 + 新适配器包(wire 协议层)。计费真相源共享。**

| 层 | 决策 |
|---|---|
| 渠道类型 | 新增 `ChannelTypeSeedance3rd = 59`(插在 `ChannelTypeDummy` 前),`ChannelTypeNames[59]` UI 显示 `seedance(第三方)`,独立 BaseURL `https://model.service-inference.ai` |
| Go 适配器包 | 新建 `relay/channel/task/seedance3rd/`(**不能叫 `seedance`,该标识符已被共享计费包占用**),与 doubao/sora 包零共享 wire 代码 |
| 计费 | 复用共享 `seedance` 包(`IsSeedance2`/`EstimateBilling`),**零改动**。客户端用原模型名 → 计费按原名查同一矩阵。`-hc`/`-ep` 是上游 wire 名,不参与计费 |
| 模型名后缀 | 客户端用原名(`dreamina-seedance-2-0-260128` 等),上游需要的 `-hc`/`-ep` 后缀由**内置渠道模型映射(`model_mapping`)**产生,零新代码(见 §2) |
| 客户端入口 | 沿用现有 `POST /v1/video/generations` 任务入口与请求体(`prompt`/`content[]`/`resolution`/`ratio`/`duration`/...),适配器内部转第三方格式 |
| 轮询 | 零改动 —— `service/task_polling.go: DispatchPlatformUpdate` 的 `default:` 分支覆盖所有数字型 platform,新渠道走通用视频轮询 |

---

## 组件设计

### 1. 新适配器包 `relay/channel/task/seedance3rd/`

#### `adaptor.go` —— 全套 `TaskAdaptor`(`relay/channel/adapter.go:34-79`)+ `OpenAIVideoConverter`

请求/响应结构(独立定义,不 import doubao):

```go
// 提交请求体(与第三方 /v1/video/generate 对齐)
type requestPayload struct {
    Model           string          `json:"model"`
    Content         []ContentItem   `json:"content,omitempty"`
    Duration        *dto.IntValue   `json:"duration,omitempty"`
    Resolution      string          `json:"resolution,omitempty"`
    Ratio           string          `json:"ratio,omitempty"`
    GenerateAudio   *dto.BoolValue  `json:"generate_audio,omitempty"`
    Watermark       *dto.BoolValue  `json:"watermark,omitempty"`
    ReturnLastFrame *dto.BoolValue  `json:"return_last_frame,omitempty"`
}
type ContentItem struct {
    Type     string    `json:"type,omitempty"`
    Text     string    `json:"text,omitempty"`
    ImageURL *MediaURL `json:"image_url,omitempty"`
    VideoURL *MediaURL `json:"video_url,omitempty"`
    AudioURL *MediaURL `json:"audio_url,omitempty"`
    Role     string    `json:"role,omitempty"`
}
type MediaURL struct{ URL string `json:"url,omitempty"` }

// 提交响应:{ "task": { "id": ... } }
type submitResponse struct {
    Task struct{ ID string `json:"id"` } `json:"task"`
}

// 查询响应:{ "task": {...} }
type fetchResponse struct {
    Task struct {
        ID           string   `json:"id"`
        Status       string   `json:"status"`
        Model        string   `json:"model"`
        Outputs      []string `json:"outputs"`
        LastFrameURL string   `json:"last_frame_url"`
        Error        *struct {
            Code    string `json:"code"`
            Message string `json:"message"`
        } `json:"error"`
        Usage struct {
            CompletionTokens int `json:"completion_tokens"`
            TotalTokens      int `json:"total_tokens"`
        } `json:"usage"`
        DurationSeconds int `json:"duration_seconds"`
    } `json:"task"`
}
```

> **CLAUDE.md Rule 6**:可选标量(`duration`/`generate_audio`/`watermark`/`return_last_frame`)用指针类型 + `omitempty`,保留显式零值语义。JSON 全走 `common.Marshal/Unmarshal`(Rule 1)。

方法实现:

| 方法 | 行为 |
|---|---|
| `Init` | 从 `RelayInfo` 取 `baseURL/apiKey/channelId/proxy/otherSettings` |
| `ValidateRequestAndSetAction` | `relaycommon.ValidateBasicTaskRequest(c, info, TaskActionGenerate)` |
| `BuildRequestURL` | `%s/v1/video/generate` |
| `BuildRequestHeader` | `Authorization: Bearer <apiKey>` + `Content-Type/Accept: application/json` |
| `EstimateBilling` | `if seedance.IsSeedance2(info.OriginModelName) { return seedance.EstimateBilling(c, info) }` else `nil`(复用共享计费) |
| `BuildRequestBody` | `convertToRequestPayload` → 模型映射 → `preuploadAssets(c, body)` → `common.Marshal` → 存 `info.UpstreamRequestBody` |
| `DoRequest` | `channel.DoTaskApiRequest(a, c, info, requestBody)` |
| `DoResponse` | 解析 `submitResponse`,取 `Task.ID` 作 upstream id;`c.JSON` 回 `dto.NewOpenAIVideo()`(id=`info.PublicTaskID`) |
| `FetchTask` | `GET %s/v1/video/tasks/{id}`,`Authorization: Bearer <key>`,走 `service.GetHttpClientWithProxy(proxy)` |
| `ParseTaskResult` | 状态映射(见下),`completed` 时取 `outputs[0]` + `usage.completion_tokens/total_tokens` |
| `ConvertToOpenAIVideo` | 回显 `outputs[0]`、`last_frame_url`、`usage`、`duration`;从 `Properties.Input` 反推 reference 计数(复刻 doubao 思路) |
| `GetModelList` / `GetChannelName` | 返回 `ModelList` / `ChannelName` |

`convertToRequestPayload`:复刻 doubao 的处理(独立一份,保隔离)——先抽 `metadata.content` 再反序列化(避免 Go slice 整体替换冲掉 `req.Images`);`req.Images` → `image_url` 条目;`duration` 优先级 `req.Seconds > metadata.duration > req.Duration`;剔除 content 中 `text` 项,用 `req.Prompt` 重建末尾 text 项。

`ParseTaskResult` 状态映射:

```
pending             → TaskStatusQueued     "10%"
processing/running  → TaskStatusInProgress "50%"
completed           → TaskStatusSuccess    "100%"  Url=outputs[0]; CompletionTokens/TotalTokens=usage.*
failed              → TaskStatusFailure    "100%"  Reason=task.error.message
default(未知)        → TaskStatusInProgress "30%"
```

#### `constants.go`

```go
// 客户端可见模型 = 原名(无后缀)。-hc/-ep 是上游 wire 名,不放这里,
// 由管理员用渠道 model_mapping 映射产生。
var ModelList = []string{
    "dreamina-seedance-2-0-260128",
    "dreamina-seedance-2-0-fast-260128",
    "dreamina-seedance-2-0-mini-260615",
}
var ChannelName = "seedance-3rd"
```

#### `asset.go` —— 素材预上传(见 §3)

#### `adaptor_test.go` / `asset_test.go` —— 表驱动测试(见验证方案)

### 2. 模型名后缀映射(内置 `model_mapping`,零新代码)

客户端用原名,上游 `-hc`/`-ep` 后缀由渠道内置模型映射产生:

- 管理员在渠道 `model_mapping` 填 `{"dreamina-seedance-2-0-260128":"dreamina-seedance-2-0-260128-hc", ...}`(不同模型可配不同后缀)。
- `helper.ModelMappedHelper` 置 `info.IsModelMapped=true`、`info.UpstreamModelName=<带后缀名>`。
- 适配器 `BuildRequestBody` 已按 doubao 同款写法处理:
  ```go
  if info.IsModelMapped { body.Model = info.UpstreamModelName } else { info.UpstreamModelName = body.Model }
  ```
  → 上游收到带后缀名。
- **计费按 `info.OriginModelName`(原名)**:`seedance.EstimateBilling` 用原名查矩阵,原三名已在矩阵中,**`pricing.go` 零改动**。有效单价仍 = `modelRatio × 2 × video_pricing`,`modelRatio` 由管理员按原模型名配置。

> 若管理员选择把某个带后缀名直接作为**客户可见模型**(而非映射目标),需另加一行同值矩阵 + `modelRatio` —— 属边缘用法,不在本次默认范围。

### 3. 素材预上传 `asset.go`(网关内部自动,按上游模型名选流程)

用 channel 开关 `seedance3rd_asset_enabled` 控制;关闭时零行为。开启时遍历 `payload.Content`,把公网 `image_url/video_url/audio_url` 上传素材库、替换为 `asset://<id>` 再提交。`asset://` 前缀幂等跳过;`data:`/base64 拒绝。鉴权直接用渠道 Bearer key。

按**上游(带后缀)模型名**选流程(`assetFlowForModel(body.Model)` —— 此时 `body.Model` 已是映射后的带后缀名):

- **hc 变体(上游名以 `-hc` 结尾)→ 简单流程**(sd_real_max.md 明确:`/v1/sd/assets` 接口只支持 hc 模型):
  - `POST /v1/sd/assets` `{URL,Name,AssetType}` → `{data:{Id}}`
  - 轮询 `GET /v1/sd/assets/{id}` 到 `data.Status == "Active"`
  - 引用 `asset://<Id>`
- **其余(260128 族 / `-ep` 等)→ 组模式**:
  - 确保有素材组:组 id 缓存于 `HybridCache`(key 含 channelId,组名 `newapi-ch<channelId>`);miss 则 `POST /v1/asset-groups` `{name,description}` → `{id}` 并缓存。
  - `POST /v1/assets` `{group_id,url,asset_type,name}` → `{id,task_id,status}`;若因组不存在报错 → 清缓存、重建组、重试一次(文档:"会轮转素材组,找不到则重建")。
  - 轮询 `POST /v1/assets/get` `{asset_id,task_id}` 到 `status == "completed"`
  - 引用 `asset://<id>`

抽象:`assetClient` 接口(`CreateAndWait(ctx, url, assetType) (assetID string, err error)`)+ 两个实现(`sdAssetClient` / `groupAssetClient`),由上游模型名后缀选择。缓存复刻 doubao `byteplus_asset.go`:`cachex.HybridCache[string]`,key = `sha256(channelId|groupId|url)`,TTL 6h,Redis 命中优先、内存回退。轮询 interval/timeout 可注入(测试用小值)。

接线点:`adaptor.go: BuildRequestBody` 在 `convertToRequestPayload` 后调用 `a.preuploadAssets(c, body)`。

### 4. 注册接线(逐处)

后端:

| # | 文件 | 改动 |
|---|---|---|
| 1 | `constant/channel.go` | 常量 `ChannelTypeSeedance3rd = 59`(`ChannelTypeDummy` 前);`ChannelBaseURLs` 追加 index 59 = `https://model.service-inference.ai`;`ChannelTypeNames[59] = "seedance(第三方)"` |
| 2 | `relay/relay_adaptor.go` | import `taskSeedance3rd "…/relay/channel/task/seedance3rd"`;`GetTaskAdaptor` 加 `case constant.ChannelTypeSeedance3rd: return &taskSeedance3rd.TaskAdaptor{}` |
| 3 | `common/endpoint_type.go` | `GetEndpointTypesByChannelType` 加 `case constant.ChannelTypeSeedance3rd: EndpointTypeOpenAIVideo`(与 Sora/AIGCVideo 对齐) |
| 4 | `controller/channel-test.go` | 59 加入 `unsupportedTestChannelTypes` |
| 5 | `setting/ratio_setting/model_ratio.go` | 无需改动(三个原模型 `dreamina-*` 已有 `defaultModelRatio`;`modelRatio` 由管理员按原名配置) |
| 6 | `dto/channel_settings.go` | `ChannelOtherSettings` 加 `Seedance3rdAssetEnabled bool` 等字段 + resolve 默认值 |

前端(两个主题都改;系统默认主题 classic):

| # | 文件 | 改动 |
|---|---|---|
| 7 | `web/default/src/features/channels/constants.ts` | `CHANNEL_TYPES` 加 `59: 'seedance(第三方)'`(顺带补漏的 `58`) |
| 8 | `web/default/src/features/channels/lib/channel-utils.ts` | vendor-icon map 加 `59`(复用 Doubao 图标或占位) |
| 9 | `web/default/src/features/channels/lib/channel-form.ts` | 素材开关字段 schema/defaults/parse/build,gate `[59]` |
| 10 | `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 素材开关 UI,gate `[59].includes(currentType)` |
| 11 | `web/classic/src/constants/channel.constants.js` | `CHANNEL_OPTIONS` 加 `{value:59,label:'seedance(第三方)',color:...}` |
| 12 | `web/classic/.../EditChannelModal.jsx` | `localModels` 加 `case 59:` 默认模型列表;素材开关 gate `[59]`(用 `handleChannelOtherSettingsChange`) |
| 13 | i18n | default 英文 key 在 en.json + `bun run i18n:sync`;classic 中文做 key,英文映射加 `web/classic/src/i18n/locales/en.json` |

---

## 已知取舍与限制

1. **重名模型共享定价**:`modelRatio` 全局按模型名查,第三方渠道的 `dreamina-seedance-2-0-260128` 与 doubao 渠道**共用同一单价**。这是"沿用上游同名模型"的既定结果。若将来要给第三方单独定价,用渠道级模型映射(映射到差异化计费别名)绕过 —— 本次不做。
2. **`-hc`/`-ep` 后缀靠管理员配置 `model_mapping`**:代码不硬编码后缀规则,不同模型可配不同后缀,由管理员在渠道后台维护。计费始终按客户端原名。素材流程按上游名后缀(`-hc` → `/v1/sd/assets`;其余 → 组模式)自动选。
3. **素材组 id:缓存 + 失效重建**(依据 sd2_real.md)。组是显式创建的、有 id(`POST /v1/asset-groups` → `{id}`),创建素材必须带 `group_id`;但文档明示"会轮转素材组,找不到则需重建"。`groupAssetClient` 逻辑:
   - 组名固定按渠道隔离(如 `newapi-ch<channelId>`),组 id 缓存于 `HybridCache`(key 含 channelId)。
   - 首次或缓存 miss → `POST /v1/asset-groups` 建组并缓存 id。
   - `POST /v1/assets` 若因组不存在报错 → 清缓存 → 重建组 → 重试一次。
   - asset_id 每次新建:`POST /v1/assets` 返回 `{id,task_id,status:"processing"}`,轮询 `POST /v1/assets/get {asset_id,task_id}` 到 `completed`,引用 `asset://<asset_id>`。
4. **`controller/task_video.go` 是死代码**(无 CAS,无调用者),不参照它;轮询逻辑以 `service/task_polling.go` 为准。

---

## 验证方案

1. **单元测试**(Go,`relay/channel/task/seedance3rd/`,表驱动):
   - `ParseTaskResult`:覆盖 `pending/processing/completed/failed/未知` → 断言 status/progress/url/tokens。
   - `convertToRequestPayload`:文生视频、单图首帧、首尾帧、多图参考、多模态(图+视频+音频)→ 断言 content 结构与 duration 优先级。
   - `DoResponse`:嵌套 `{task:{id}}` 正确取 id;id 为空报错。
   - `asset.go`:上游名 `-hc` 结尾 → 走 `/v1/sd/assets` 并轮询 Active;其余 → 走组模式并轮询 completed;组失效时清缓存重建组并重试一次;`asset://` 幂等跳过;缓存命中不重复上传(用 httptest 计数)。
2. **模型映射测试**(Go):`IsModelMapped` 时 `body.Model = UpstreamModelName`(带后缀);`EstimateBilling` 仍用 `OriginModelName`(原名)查矩阵得出正确 `video_pricing`。
3. **构建**:`go build ./...` 通过;`go vet ./relay/channel/task/seedance3rd/...` 无新增告警。
4. **端到端**(手动):建渠道类型 59、填 Bearer key,配三个原模型 modelRatio,并配 `model_mapping` 追加 `-hc`/`-ep`;分别提交文生/图生/首尾帧/含视频输入任务,轮询完成后核对:
   - 上游实际收到带后缀模型名;扣费按原名 `quota ≈ completion_tokens × modelRatio × groupRatio × video_pricing ÷ 500000`;
   - 使用记录详情弹窗「视频计费」区块单价/token/分组倍率/公式/实际扣费一致;
   - 含公网媒体且开启素材开关时,上游收到 `asset://<id>`(hc 走 sd/assets,其余走组模式)。
5. **前端**:`cd web/default && bun run build` 通过;两主题都能选到 `seedance(第三方)` 渠道并配置素材开关。
6. **回归**:doubao(45/54)与 sora(55/1)的 Seedance 计费与日志行为完全不变(共享 `seedance` 包**零改动**)。
