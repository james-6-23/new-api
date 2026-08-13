# 豆包/火山渠道(type 45)第三方接入与素材库双协议设计

日期:2026-08-10
范围:渠道类型 45(`ChannelTypeVolcEngine`,UI 名"豆包")放开 base_url 限制以接入第三方上游;素材库从单一 BytePlus 官方协议扩展为**官方 + 第三方 cloudwise 双协议**;素材组容量耗尽时按上游报错自动创建新组并回写渠道配置。

上游接口文档:`接口文档/cloudwise-seedance接入文档/maas seedance2.0调用说明 0710.docx`(第三方 cloudwise maas)、`接口文档/海外byteplus-seedance文档/CreateAsset API 文档.md`(官方 BytePlus)。

---

## Context(为什么做这件事)

渠道 45 是一个**双适配器渠道类型**:

- 同步中继(chat/embedding/image/rerank/responses/audio)走 `relay/channel/volcengine.Adaptor`(`common/api_type.go:54-55` → `relay/relay_adaptor.go:101-102`)。
- 任务/视频请求走 `relay/channel/task/doubao.TaskAdaptor` —— 与渠道 54 共用同一适配器(`relay/relay_adaptor.go:156-157`):

```go
case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
    return &taskdoubao.TaskAdaptor{}
```

因此渠道 45 的视频请求**完全可以走到**素材库预上传逻辑 `doubao/byteplus_asset.go:259` `preuploadAssets`,链路无渠道类型过滤。

当前有三处阻碍第三方接入:

1. **前端 base_url 被限制为两个官方地址**。`web/default` 的 `channel-mutate-drawer.tsx:1935-1994` 与 `web/classic` 的 `EditChannelModal.jsx:3722-3752` 都渲染一个二选一 `Select`(`https://ark.cn-beijing.volces.com` / `https://ark.ap-southeast.bytepluses.com`)。存在一个连点 10 次标签解锁自由输入的隐藏后门(`channel-mutate-drawer.tsx:415-431`、`EditChannelModal.jsx:520-532`),说明这个限制本身就已被视为需要绕开。后端无任何 base_url 校验(`controller/channel.go:459-516` `validateChannel` 无 type 45 分支)。

2. **素材库只有官方一套协议**。`byteplus_asset.go:79-84` 把 host 写死为 `https://ark.{region}.byteplusapi.com`,鉴权是 Volcengine HMAC-SHA256 AK/SK 签名(`:207`)。第三方 cloudwise 是 Bearer token + `/api/v1/assets/*` 路径 + `{code,message,data}` 信封,两套形状不兼容。

3. **素材组是管理员手填的静态单值**,从不自动创建。`byteplus_asset.go:264-266` 要求 `BytePlusAssetGroupId` 非空,`:280` 直接把它塞进 client。组容量耗尽后管理员必须手动到上游建组再回来改配置。

**目标产出**:渠道 45 可配置任意上游地址;素材库支持官方/第三方两套协议且由管理员显式选择;素材组因容量耗尽报错时自动建新组、重试并回写配置。

---

## 决策总览

| 层 | 决策 |
|---|---|
| 配置 | `dto.ChannelOtherSettings` 新增**单个**字段 `AssetProvider`,空值 == `byteplus`,存量渠道零迁移 |
| 素材库分层 | 抽 `assetClient` 接口,与仓库既有的 `seedance3rd/asset.go:32` 同构;doubao 包内切成三个文件 |
| 建组时机 | **仅**当上游返回 asset 组相关错误(`IsGroupExhausted` 为真)时创建,不做预防性建组 |
| 建组上限 | 每请求最多创建一个新组 |
| 回写 | 新 group_id 定点更新 `settings` 单列 + `CacheUpdateChannel`,不走 `channel.Update()` |
| 并发 | 不加分布式锁,接受偶发空组(理由见 §3) |
| 前端 | 两个主题都改:base_url 换可编辑下拉、删隐藏后门、素材库配置区加 provider 分支 |
| 连带修复 | `volcengine/adaptor.go:277` TTS 的 base_url 全等判断改为 host 归属判断 |

**明确不做**:不重命名 `byteplus_*` 字段(避免存量迁移);不统一两个前端对 `doubao-coding-plan` 的既有行为分歧;不改动 `seedance3rd` 包。

**关于渠道 54**:它与 45 共用 `doubao.TaskAdaptor`(`relay/relay_adaptor.go:156-157`),因此**素材库的改动对 54 同样生效** —— 54 会一并获得自动建组能力。这是有意的:两者本就共用同一套素材库代码,分叉出两种行为反而是缺陷。54 不加 provider 下拉(前端不改),其 `AssetProvider` 恒为空即 `byteplus`,协议行为与当前完全一致。

---

## 1. 配置模型

`dto/channel_settings.go`(现有 BytePlus 字段在 `:66-72`)新增:

```go
// AssetProvider 选择素材库协议实现:
//   "" / "byteplus" —— 官方 BytePlus OpenAPI(AK/SK 签名,ark.{region}.byteplusapi.com)
//   "cloudwise"     —— 第三方 cloudwise maas(Bearer + 渠道 base_url + /api/v1/assets/*)
AssetProvider string `json:"asset_provider,omitempty"`
```

空值等价 `byteplus`,存量渠道行为不变。

各字段在两种 provider 下的归属:

| 字段 | 官方 byteplus | 第三方 cloudwise |
|---|---|---|
| 素材库 host | `ark.{region}.byteplusapi.com`(由 region 推导) | **复用渠道 base_url** |
| 鉴权 | `byteplus_access_key` + `byteplus_secret_key` 签名 | **复用渠道 Bearer key** |
| `byteplus_region` | 用 | 不用(前端隐藏) |
| `byteplus_project_name` | 用 | 不用(文档 create 请求无此字段) |
| `byteplus_asset_group_id` | 起始组 / 回写目标 | 起始组 / 回写目标 |
| `byteplus_moderation_skip` | 用 | 用(文档同样有 `Moderation.Strategy`) |

三项取舍:

1. **第三方不加独立素材库 endpoint 字段**,直接复用 base_url。文档中生成与素材库同 host(`api.cloudwise.ai`)。未来若拆 host 再加,现在加属 YAGNI。
2. **不重命名 `byteplus_*` 字段**。语义上它们已泛化为"素材库配置",名字带 `byteplus` 是命名债;但改 JSON key 需为存量渠道写迁移,收益不抵风险。保留原名,注释说明。
3. **`byteplus_asset_group_id` 由必填改为选填**。有自动建组能力后可留空;`channel-form.ts:288-312` 的必填校验相应放开,但 provider=byteplus 时 AK/SK 仍必填。

`ResolveBytePlusAsset()`(`dto/channel_settings.go:88-104`)保持不变,新增一个 `ResolveAssetProvider()` 返回归一化后的 provider 字符串(空 → `byteplus`)。

---

## 2. 素材库分层与文件切分

现有 `preuploadAssets`(`byteplus_asset.go:259-320`)把三件事焊死在一起:开关与配置校验、协议构造、URL 遍历替换。加入第二套协议前先按职责切开。

### 接口(定义在 `asset.go`,协议无关层)

```go
// assetClient 是素材库协议实现的统一入口。
type assetClient interface {
    // CreateAndWait 上传单个公网媒体 URL 到指定组,轮询到可用后返回 assetId。
    CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error)
    // CreateGroup 新建素材组,返回新 groupId。
    CreateGroup(ctx context.Context, name string) (string, error)
    // IsGroupExhausted 判断该错误是否为"组已满/组不可用",决定是否值得换组重试。
    IsGroupExhausted(err error) bool
}
```

与现有实现的两处签名差异,均为让编排层能管理组:

- `groupID` 从 client 字段(`bytePlusAssetClient.groupId`,`byteplus_asset.go:50`)**提升为方法参数**。否则换组必须重建 client。
- `IsGroupExhausted` 由**协议实现**负责。只有它认得自身上游的错误码形状;编排层不应 match 错误字符串。

该接口与仓库既有的 `relay/channel/task/seedance3rd/asset.go:32` `assetClient` 同构(那里已有 `sdAssetClient`/`groupAssetClient` 两个实现),属于复用已验证的结构而非新发明。

### 文件职责

| 文件 | 内容 | 行数估计 |
|---|---|---|
| `doubao/asset.go`(新) | 接口定义、provider 工厂与前置校验、`preuploadAssets` 编排、`pickMedia`、URL→assetId 缓存(由 `byteplus_asset.go:253-380` 整段迁入)、建组重试与回写 | ~200 |
| `doubao/byteplus_asset.go`(改) | 仅保留官方 AK/SK 实现:`volcsign` 签名、`createAssetRequest`、`bytePlusEnvelope`、`CreateAsset`/`GetAsset` 轮询 | ~280 |
| `doubao/cloudwise_asset.go`(新) | 第三方 Bearer 实现 | ~220 |

### provider 工厂

```go
func (a *TaskAdaptor) newAssetClient(httpClient *http.Client) (assetClient, error)
```

`asset.go` 内唯一 switch provider 的地方,同时承担**分 provider 的前置校验**:官方要求 AK/SK 非空,第三方要求 base_url 与渠道 key 非空。现有校验(`byteplus_asset.go:264-266`)是一刀切的,第三方走进来会因未填 AK/SK 直接报错。

### 第三方 cloudwise 协议(依 maas 文档)

| 操作 | 请求 | 响应 |
|---|---|---|
| 建组 | `POST {base}/api/v1/assets/groups/create`,body `{name, description}` | `{ResponseMetadata, Result:{Id}}` |
| 上传 | `POST {base}/api/v1/assets/create`,body `{name, description, assetType, groupId, url, Moderation:{Strategy}}` | `{code:10000, message, data:{id}}` |
| 查详情 | `POST {base}/api/v1/assets/get`,body `{Id}` | `{ResponseMetadata, Result:{Id, Status, ...}}` |

注意上游信封**不统一**:建组/查详情用 PascalCase 的 `ResponseMetadata`/`Result`,上传用小写 `{code,message,data}`。实现需分别解析,不可共用一个信封结构。轮询判定 `Result.Status == "Active"`,与官方一致。

### 缓存 key 调整

现为 `channelId|groupId|project|url`(`byteplus_asset.go:336-339`)。组会动态变化,把 groupId 留在 key 中会导致换组后同一 URL 重复上传。改为:

```
channelId|provider|project|url
```

同一渠道同一素材只上传一次,落在哪个组无关。cachex namespace 由 `byteplus_asset` 改为 `asset`。这会让存量缓存全部失效,但 TTL 仅 6 小时(`assetCacheTTL`,`:255`)且失效后果只是多传一次,故直接改,不做兼容读。

---

## 3. 建组重试与回写

### 触发条件

**唯一触发点**:`CreateAndWait` 返回错误,且 `IsGroupExhausted(err)` 为真。除此之外一律不建组。

不做"group_id 为空则预先建组"——那是预防性建组,浪费资源。group_id 为空时照常发起上传,让上游报错,报错后才建。

### 编排逻辑(`asset.go`,替换现有 `byteplus_asset.go:312-317`)

```
activeGroup  := settings.BytePlusAssetGroupId   // 可能为空
groupCreated := false

for 每个媒体 URL:
    if 命中缓存 → media.URL = asset://id; continue
    if 已是 asset:// 前缀 → 幂等跳过
    if 非 http(s) → 报错(base64/data URI 不支持)

    assetID, err := cl.CreateAndWait(ctx, activeGroup, url, assetType)

    if err != nil && cl.IsGroupExhausted(err) && !groupCreated:
        activeGroup  = cl.CreateGroup(ctx, fmt.Sprintf("newapi-ch%d", channelId))
        groupCreated = true                      // 本请求内不再建第二个组
        assetID, err = cl.CreateAndWait(ctx, activeGroup, url, assetType)

    if err != nil → return err
    缓存并替换 media.URL = asset://assetID

if groupCreated → 异步回写 activeGroup 到渠道 settings
```

硬约束:

- **每请求最多建一个新组**。用 bool 而非计数器:组满是低频事件,同一请求内连续两次组满几乎只能是 `IsGroupExhausted` 误判,此时应报错而非继续刷组。
- 仅 `IsGroupExhausted` 为真才换组。

### `IsGroupExhausted` 的判定范围

这是省资源的关键,判宽了会滥建组。仅对**组本身不可用**返回 true:

- 组容量已满
- 组不存在 / 组 id 无效(含 group_id 为空导致上游拒绝)
- 组状态异常不可写

**明确不触发建组**:鉴权失败、URL 不可达、媒体格式不支持、内容审核拒绝、轮询超时、`Status=Failed`(`byteplus_asset.go:136-142` 路径)。这些换组重试结果相同,只会白建。

实现上两套 provider 各判各的。官方看 `ResponseMetadata.Error.Code` —— 现有代码 `:237-238` 已取出 Code 但拼进字符串就丢失了结构,**需定义带 Code 字段的错误类型**,不让上层 match 字符串。第三方看 `{code,message}`。

**已知限制**:BytePlus CreateAsset 文档未列错误码表,组满的确切 Code 未知。因此判定表初始按已知的组相关 Code 关键词匹配,**判不准时默认返回 false**——宁可不建组、把上游原始错误直接报给管理员,也不滥建。错误原文完整记入日志,管理员遇到真实组满错误时可看到确切 Code 再补进判定表。此设计下即使判定表不全,行为也退化为当前行为(报错),不会刷组。

### 回写

成功建组后异步执行:

```go
// 定点更新 settings 单列
DB.Model(&Channel{Id: id}).Select("settings").Updates(...)
model.CacheUpdateChannel(ch)   // model/channel_cache.go:251
```

不使用 `channel.Update()`(`model/channel.go:533`)——它是全量 `Updates(channel)` 加 `UpdateAbilities`,在中继热路径上会覆盖管理员并发编辑并重建 ability 表。参照 `UpdateResponseTime`(`:583`)的定点更新模式。

回写前先 `GetChannelById` 重读拿最新 settings,再只改 `byteplus_asset_group_id` 一个字段,压缩覆盖窗口。

**注意 `CacheUpdateChannel` 的前置条件**:该函数在 `!common.MemoryCacheEnabled` 时直接返回(`model/channel_cache.go:252-254`),且要求传入**完整的 Channel 对象**(它整体替换 `channelsIDM[id]`,`:267`)。因此回写路径必须传重读得到的完整对象,不能传一个只填了 Id 与 settings 的裸结构 —— 否则内存缓存中该渠道的其余字段会被清空。未开内存缓存时 DB 已更新,下次读取自然生效。

### 并发取舍(明示)

回写是 read-modify-write。两个并发请求同时遇到组满会各建一个组、后写覆盖先写。**不加分布式锁**,理由:

- 后果是上游多一个空组,无实际损害
- 被覆盖的组 id 丢失,但该组内素材已通过 URL→assetId 缓存可用,素材本身不因组指针丢失而失效
- 加锁需引入跨实例协调,复杂度远超收益

代价是上游可能积累少量空组。重读机制压缩了窗口但不消除它。

### 可观测性

每次自动建组记一条 WARN 日志,带 channelId、旧组 id、新组 id、触发错误原文。管理员不会莫名发现配置被改动。

---

## 4. 前端与 base_url 放开

### base_url 控件

两个主题一致:type 45 的 base_url 由二选一 `Select` 换为**可编辑下拉**,两个官方地址保留为预设项(一键选中),同时允许自由输入。

- `web/default`:Combobox(Base UI),替换 `channel-mutate-drawer.tsx:1935-1994` 的锁定分支
- `web/classic`:Semi `Select` 开 `filter` + `allowCreate`,替换 `EditChannelModal.jsx:3722-3752`

**删除 10 次点击隐藏后门**:`channel-mutate-drawer.tsx:415-431` 的 hook 调用与 `:1997-2019` 解锁分支;classic 的 `:520-532` 与 `:3546`。其存在的唯一理由就是绕开二选一限制,限制取消后即为死代码。

`web/default/src/hooks/use-hidden-click-unlock.ts` 全仓库**仅此一处调用方**(已确认),移除后该 hook 文件成为死代码。本次一并删除该文件;classic 的解锁逻辑是手写的,随调用点一起移除。

classic 的 `doubao-coding-plan` 灰化项保持原样(`:3742-3747` 的 `disabled: !canKeepDeprecatedDoubaoCodingPlan` grandfathering)。两个前端在该项上行为本就不一致(default 静默改写,classic 灰化保留),属既有分歧,不在本次范围内统一。

### 素材库配置区

`channel-mutate-drawer.tsx:1467-1626` 与 classic 对应块加 provider 下拉,按选择切换字段可见性:

| provider | 显示 | 隐藏 |
|---|---|---|
| 官方 BytePlus | AK、SK、region、project、group_id、moderation | — |
| 第三方 cloudwise | group_id、moderation | AK、SK、region、project |

第三方分支需附说明文案:素材库复用本渠道的 base_url 与 API Key。

### 校验放开

`channel-form.ts:288-312` 现在对 `[45, 54]` 且开启素材库时,无条件要求 AK/SK/group_id 三者非空。改为:

- AK/SK:**仅 provider=byteplus 时**必填
- group_id:两种 provider 下均改为选填(配合 §3 的自动建组)
- provider=cloudwise 时改为校验 base_url 非空(素材库复用它)

注意该校验块同时覆盖渠道 54。渠道 54 无 provider 下拉(不在本次前端范围内),其 provider 恒为空值即 `byteplus`,故走 AK/SK 必填分支,行为不变;但 group_id 转为选填对 54 同样生效 —— 这是有意的,54 与 45 共用同一套素材库代码,自动建组能力对两者一致可用。

### 连带修复:TTS

`volcengine/adaptor.go:276-280` 现按 base_url **字符串全等**切换 WebSocket:

```go
case constant.RelayModeAudioSpeech:
    if baseUrl == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
        return "wss://openspeech.bytedance.com/api/v1/tts/ws_binary", nil
    }
    return fmt.Sprintf("%s/v1/audio/speech", baseUrl), nil
```

放开自由输入后,任何自定义地址的 TTS 都会静默打到 `{base}/v1/audio/speech` —— 方舟不提供该路径,用户会得到难以理解的错误。

改为**按 host 归属判断**:host 属于官方域(`volces.com` / `bytepluses.com`)走 WebSocket,否则走 `{base}/v1/audio/speech`。这比全等判断更准——带 path 或换 region 的官方地址也能正确走 TTS。`DoRequest`(`adaptor.go:332-345`)中的同一相等测试需同步修改。

这是本次需求之外的缺陷修复(选 bytepluses 那项时当前同样会中),纳入本次范围是因为放开输入会把它从"少见"变为"常见"。

---

## 5. 错误处理与测试

### 错误分层

素材库失败发生在 `BuildRequestBody` 阶段(`doubao/adaptor.go:176`),此时尚未请求上游生成接口,属**预检失败**,请求不应计费。现有行为(返回 error → `BuildRequestBody` 失败 → `relay_task.go` 走错误路径)正确,保持不变。

错误信息按三类区分,使管理员可自行定位:

| 类别 | 呈现 |
|---|---|
| 配置缺失(provider=byteplus 但无 AK/SK) | 明确指出缺失字段名 |
| 组相关(建组重试后仍失败) | 带原始 Code 与两次尝试的组 id |
| 其他上游错误 | 原文透传(截断 512)并脱敏 |

第三类需补脱敏:现有 `byteplus_asset.go:241` 的 `truncate` 直接把响应体拼入 error,**未脱敏**;而 `seedance3rd/adaptor.go:221` 已走 `common.MaskSensitiveInfo`。素材库错误可能含签名 URL,需补上脱敏,与既有 spec `2026-07-31-task-upstream-error-passthrough-design.md` 口径对齐。

### 测试

表驱动,沿用 `byteplus_asset_test.go` 的 `httptest.Server` 模式。

`doubao/asset_test.go`(新,编排层,两 provider 共用):

- 命中缓存 → 不发任何 HTTP 请求
- `asset://` 前缀 → 幂等跳过
- `data:` / base64 → 报错且不发请求
- **组满 → 建组一次 → 重试成功**(核心路径)
- **组满 → 建组 → 仍组满 → 报错,且全程只建了一个组**(验证每请求一次上限)
- **非组相关错误(鉴权失败 / 审核拒绝)→ 不建组**(验证省资源)
- 回写:建组后 settings 被更新且 `CacheUpdateChannel` 被调用

`doubao/cloudwise_asset_test.go`(新,第三方协议):

- 建组 / 上传 / 轮询至 Active 的完整往返
- 两种信封分别解析(`ResponseMetadata`/`Result` 与 `{code,message,data}`),含 code≠10000 错误路径
- Bearer 头正确,且用的是渠道 key 而非 AK/SK

`doubao/byteplus_asset_test.go`(改):现有用例保留,签名适配 groupID 提参。

前端:`channel-form.ts` 校验分支加单测(provider=cloudwise 时不要求 AK/SK)。实现时确认前端测试框架现状,无框架则以手工走查替代。

### 端到端验证

单测之外,起服务配一个 cloudwise 渠道跑通一次带参考图的视频生成。素材库这条链路的真实上游字段大小写与信封形状(尤其上传接口的小写信封)是 mock 测试覆盖不到的。

---

## 影响面清单

| 层 | 文件 | 动作 |
|---|---|---|
| 配置 | `dto/channel_settings.go` | 加 `AssetProvider` 字段 + `ResolveAssetProvider()` |
| 素材库 | `relay/channel/task/doubao/asset.go` | 新建:接口、工厂、编排、建组重试、缓存、回写 |
| | `relay/channel/task/doubao/byteplus_asset.go` | 瘦身为纯官方实现;补带 Code 的错误类型;补脱敏 |
| | `relay/channel/task/doubao/cloudwise_asset.go` | 新建:第三方 Bearer 实现 |
| TTS | `relay/channel/volcengine/adaptor.go:277,332-345` | 全等判断 → host 归属判断 |
| 前端 | `web/default/.../channel-mutate-drawer.tsx` | Combobox、provider 下拉、删后门 |
| | `web/classic/.../EditChannelModal.jsx` | 同上 |
| | `web/default/.../lib/channel-form.ts` | 校验按 provider 分支 |
| i18n | `web/default/src/i18n/locales/*.json` | 新增文案键 |
| 测试 | `doubao/asset_test.go`、`doubao/cloudwise_asset_test.go` | 新建 |
| | `doubao/byteplus_asset_test.go` | 适配签名变更 |

**不改动**:`seedance3rd` 包;`byteplus_*` 字段名;两个前端对 `doubao-coding-plan` 的既有分歧。

**渠道 54 的连带影响**:54 与 45 共用 `doubao.TaskAdaptor`,故素材库的分层重构与自动建组对 54 一并生效(有意为之,见"决策总览")。54 的前端不加 provider 下拉,协议行为不变。回归测试需覆盖 54 的既有素材库路径。
