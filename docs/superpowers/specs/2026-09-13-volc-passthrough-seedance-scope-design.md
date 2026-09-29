# 字节火山透传渠道（type 60）收缩为 Seedance + 私域素材库

2026-09-13

## 背景

type 60 现状是**通配全量透传**：数据面放行 `/api/v3/`、`/api/compatible/`、
`/api/coding/` 三个宽前缀，控制面放行任意 `?Action=`。客户拿站点令牌可以调到
chat/completions、embedding 等全部官方能力，而这些路径不计费。

本次收缩为只提供两组能力：**Seedance 视频生成** 与 **私域素材库（含真人认证）**，
并新增**按用户的资产隔离**与**资产管理页面**。

## 一、接口白名单

白名单外一律拒绝，返回站点自身错误形状 `{"error":{...,"type":"new_api_error"}}`
配 404 —— 不伪装成官方错误，客户能分清是站点挡的还是火山挡的。

### 数据面（4 条，Bearer 站点令牌）

| 方法 | 路径 | 计费 |
|---|---|---|
| POST | `/api/v3/contents/generations/tasks` | 是 |
| GET | `/api/v3/contents/generations/tasks/{id}` | 否 |
| GET | `/api/v3/contents/generations/tasks` | 否 |
| DELETE | `/api/v3/contents/generations/tasks/{id}` | 否 |

`allowedPathPrefixes` 由三个宽前缀改为精确匹配上表（含 `{id}` 变量段）。
`/api/compatible/`、`/api/coding/` 整组删除。

> **不可逆点**：删除后若要给客户开 chat/completions，须改代码。已与用户确认按硬封禁执行。

### 控制面（12 个 Action，AK/SK 签名）

资产组：`CreateAssetGroup` `GetAssetGroup` `ListAssetGroups` `UpdateAssetGroup` `DeleteAssetGroup`
资产：`CreateAsset` `GetAsset` `ListAssets` `UpdateAsset` `DeleteAsset`
真人认证：`CreateVisualValidateSession` `GetVisualValidateResult`

控制面由「任意 Action 放行」改为只认这 12 个。

### 路径方案（不变）

数据面挂站点根，与官方逐字一致：`<站点>/api/v3/contents/generations/tasks`。
控制面保留 `/volc` 前缀（官方挂域名根，站点根被前端占用）：`<站点>/volc/?Action=X&Version=2024-01-01`。

## 二、资产归属与隔离

上游一个渠道只有一套 AK/SK，所有站点令牌在火山侧是同一个资产命名空间；
官方 `ProjectName` 是 IAM 预建概念，无法按令牌动态开。故隔离由 new-api 自己记账。

**归属粒度：`user_id`**（同一用户的多个令牌共享资产，客户轮换令牌不丢素材；跨用户隔离）。

### 表 `volc_assets`

GORM 定义，须同时兼容 SQLite / MySQL ≥5.7.8 / PostgreSQL ≥9.6（Rule 2）。

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | 自增主键 | |
| `user_id` | int | 归属用户，隔离依据 |
| `channel_id` | int | 上游渠道，跨渠道同 id 不互通 |
| `resource_type` | string | `asset` / `asset_group` |
| `resource_id` | string | 官方 Id，如 `asset-20260410114236-8cdfz` |
| `group_id` | string | 资产所属组（资产行才有） |
| `name` | string | 快照 |
| `asset_type` | string | 快照，Image / Video / Audio |
| `status` | string | 快照，Queued / Processing / Active / Failed |
| `created_at` / `updated_at` | 时间戳 | |

唯一索引 `(channel_id, resource_type, resource_id)`；查询索引 `(user_id, channel_id, resource_type)`。
注册进 `model/main.go` 的 `DB.AutoMigrate` 列表。

### 写入时机

仅在转发**成功后**从官方响应取 Id 落库：

- `CreateAsset` → `resource_type=asset`，快照 name / asset_type / status
- `CreateAssetGroup` → `resource_type=asset_group`
- `GetVisualValidateResult` → 返回的 `GroupId` 落为 `asset_group`（真人认证换来的真实资产组，必须纳入隔离）
- `CreateVisualValidateSession` 不落库（返回的是 30 分钟有效的临时 BytedToken）

### 校验时机

1. **带 `Id` 的单资源 Action**（`GetAsset` `UpdateAsset` `DeleteAsset` `GetAssetGroup`
   `UpdateAssetGroup` `DeleteAssetGroup`）：转发前查表，不属于当前 `user_id`+`channel_id`
   直接拒，**不发上游**。
2. **List 类**（`ListAssets` `ListAssetGroups`）：转发后按归属过滤 `Result.Items`，重算 `TotalCount`。
3. **`CreateAsset`**：请求体 `GroupId` 必须属于自己，否则拒（否则客户能往别人的组塞素材）。
4. **视频提交**：`POST /api/v3/contents/generations/tasks` 扫 `content[]` 全部 `url`，
   凡 `asset://<id>` 逐个校验归属，一个不属于自己就整单拒。**这是隔离的闭环** ——
   否则猜到别人的 asset id 即可直接生成视频，前三条校验全部失效。

### 快照刷新

第 1、2 类校验本来就要解析上游响应，顺手用响应里的 name / asset_type / status
刷新本地快照，零额外调用。客户轮询越勤，快照越新。

### 拒绝措辞

统一 404，且**不区分「不存在」与「不属于你」** —— 避免站点被当作 id 探测器。

### 已知取舍

- **List 分页条数不准**：官方按上游全量分页，站点过滤后每页少于 `PageSize`。
  文档写明「分页以 `NextToken` 为准，不要依赖单页条数」。
- **孤儿资产**：落库失败时上游已建好、本地无记录，客户看不见但额度照占。
  落库失败打 error 日志带 `resource_id`；超管页面的「上游已用 vs 本地记账」差值即孤儿数。

## 三、计费与内容过滤

**只有 `POST /api/v3/contents/generations/tasks` 计费**，走既有 `RelayTask` 链路，
模型名由客户传，分组/倍率/预扣退款与其他渠道一致。12 个 Action 与 3 条任务查询/删除
只校验令牌与归属，不扣额度。分辨率档位沿用 `relay/channel/task/seedance/pricing.go`，不动。

**内容过滤不做定制**：`Moderation.Strategy` 原样透传，站点不注入、不改写、不拦。
文档须原文引用官方定义 —— `Skip` 是 *"Skip most non-baseline content security review
policies"*，**跳过大部分非基线策略，不是全量绕过**，基线策略照旧生效。

**唯一的请求改写**：`ProjectName` 客户未传时注入渠道配置的项目名。官方要求
`CreateAsset` 的 `ProjectName` 必须与目标资产组所属项目一致，而项目是站点 IAM 账号
下的概念，客户无从知晓。客户传了用客户的。文档须标注此处。

### 未加限制的口子（记录，本期不做）

- 素材上传免费，客户可只 `CreateAsset` 不生成视频，存储成本落在站点火山账号。
- 真人认证 H5 的 `CallbackURL` 由客户传，回调打到客户自己地址，站点不介入（官方设计）。

## 四、资产管理页面

### 额度数据的硬约束

**官方没有查素材限额的 API。** 全量 Action 枚举后，资产相关只有 12 个 CRUD；
`GetAFPUsage` / `GetInferenceUsage` / `ListModelRateLimit` 都是模型调用量与限流。
素材总量、素材资产上限、利用率官方只在控制台看板给（`素材库数据看板和告警设置.md:17`）。

故：**上限由管理员在渠道配置手填**（`ChannelOtherSettings` 新增 `asset_quota_limit`，
0 表示未知/不限）；**已用量取官方 `ListAssets` 的 `TotalCount`**（权威值，含孤儿）。

### 后端接口（站点管理 API，非透传路径）

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/api/volc_asset/self` | 登录用户，仅自己 |
| GET | `/api/volc_asset/` | 超管，全部用户，可按 user_id / channel_id / status 筛 |
| GET | `/api/volc_asset/quota` | 超管，各渠道额度概览 |
| POST | `/api/volc_asset/sync` | 超管，从官方 ListAssets 批量刷新快照并发现孤儿 |
| DELETE | `/api/volc_asset/:id` | 超管，先删上游再删本地行 |

删除失败时**保留本地行**并报错，避免删出新的孤儿。

### 页面

- **用户视图**：自己的资产列表 —— 资产 ID（一键复制）、名称、类型、所属组、状态、创建时间。
- **超管视图**：额外含用户列、渠道列、删除按钮，以及额度卡片
  （上限 / 上游已用 / 本地记账 / 孤儿数）。

两个主题都要加页面与路由：classic 用 Semi `Table`，default 用 Base UI。
渠道配置新增 `asset_quota_limit` 须动四处（schema / defaults / parse / build）。
前端改动后须重新编译两个主题并重打 `new-api.exe`（`go:embed`）。

## 五、文档

`cn-client-api.md` 与 `overseas-client-api.md` 按白名单重写，各只剩 4 条数据面 +
12 个 Action，共 16 个 curl，仍 `export SITE=` / `export TOKEN=` 设一次全篇可跑。

每篇新增三节：

- **完整工作流串联**：建组 → 上传素材 → 轮询到 `Active` → `asset://<id>` 提交视频 →
  轮询任务 → 取结果。一串可从头复制到尾的 curl，中间用 shell 变量接 id。
- **隔离行为说明**：归属规则、404 的两种含义、List 分页条数不可依赖。
- **分辨率支持矩阵**：四个模型各支持哪些档位；`resolution` 用结构化字段而非 prompt 里的 `--rs`。

`cn-channel-setup.md` / `overseas-channel-setup.md` 补项目名与额度上限怎么填。
README 索引表同步改数字。

## 六、验证

Go 单测四组：

1. 路径与 Action 白名单：放行 16 项，拒绝 chat/completions、embedding 等代表性路径
2. 归属表增删查，含跨渠道同 id 不互通
3. 三类校验分支：单资源拒、List 过滤、`CreateAsset` 的 GroupId 校验
4. `asset://` 提取与校验：混合 content、大小写、非 asset 协议不误伤

`go test ./middleware/ ./router/ ./relay/channel/volcpassthrough/ ./model/ ./controller/`
加 `go vet`；前端 `bun run typecheck`。
