# 字节火山透传渠道（Channel Type 60）

把火山方舟（国内）/ BytePlus ModelArk（海外）的 **Seedance 视频生成 + 私域素材库**
原样透出给客户：路径、query、请求体、响应体、状态码、错误结构一律不改，
客户端只需要改两处 ——

| | 官方直连 | 走本站点 |
|---|---|---|
| 数据面 | `https://ark.cn-beijing.volces.com/api/v3/...` | `https://<你的站点>/api/v3/...` |
| 控制面 | `https://ark.cn-beijing.volcengineapi.com/?Action=...` | `https://<你的站点>/volc/?Action=...` |
| 凭据 | 火山 API Key / AK·SK | 本站点令牌 `sk-xxxx` |

**数据面路径与官方完全一致，只换域名。** 官方 SDK 只改 `base_url` 即可直接使用。
控制面因为官方挂在域名根路径上（`https://<host>/?Action=`），而站点根路径被前端页面占用，
所以保留 `/volc` 前缀。`/volc/api/v3/...` 也仍然可用，便于统一到单一前缀。

AK、SK、API Key、项目名、区域只存在本站点的渠道配置里，不下发给客户。

## 文档索引

| 文档 | 面向 | 内容 |
|---|---|---|
| [国内 · 渠道配置](./cn-channel-setup.md) | 站点管理员 | 火山方舟渠道怎么建、AK/SK 填哪、区域与控制面 host 怎么定 |
| [国内 · 客户端调用](./cn-client-api.md) | 调用方 | 4 条数据面接口 + 12 个控制面 Action，**每个都带可直接复制的 curl 测试用例** |
| [海外 · 渠道配置](./overseas-channel-setup.md) | 站点管理员 | BytePlus ModelArk 渠道配置，含 `ap-southeast` / `ap-southeast-1` 陷阱 |
| [海外 · 客户端调用](./overseas-client-api.md) | 调用方 | 同上 16 个接口的海外版本，同样每个一个 curl 用例 |
| [素材库管理接口](./asset-admin-api.md) | 站点前端 / 管理员 | `/api/volc_asset/*` 五条站点内部接口：我的资产、全部资产、额度对账、回源同步、删除 |
| [Apifox 测试集合 · Postman 版](./apifox-seedance2.postman_collection.json) | 测试 | Postman Collection v2.1.0，导入 Apifox 时选「**Postman**」：16 个透传入口 + 5 条站点管理接口 + 端到端串联 + 6 条反向用例，变量自动串联 |
| [Apifox 测试集合 · OpenAPI 版](./apifox-seedance2.openapi.json) | 测试 | OpenAPI 3.0.0（已通过规范校验），导入 Apifox 时选「**OpenAPI/Swagger**」。控制面是一条 `POST /volc/`，`Action` 做成 12 个带中文标签的下拉枚举，与官方 RPC 形态一致 |
| [Apifox 测试用例说明](./apifox-test-cases.md) | 测试 | 上面两份集合共用的中文速查表：每个用例的参数、预期状态码、断言与要点，含反向用例与计费口径 |

> 两个集合文件对应 Apifox 的**两个不同导入器**。选错格式会报
> "请检查是否选择了正确的数据格式及数据文件" —— 按你要用的导入器选对应文件即可，两份内容等价。

## 核心设计

**白名单精确放行，不做通配转发。** 只有下面 16 个入口可用，其余官方接口一律
`404 endpoint_not_supported`：

| 面 | 放行内容 |
|---|---|
| 数据面 | `POST` / `GET` `/api/v3/contents/generations/tasks`、`GET` / `DELETE` `…/tasks/{id}` |
| 控制面 | 素材组 5 个 + 素材 5 个 + 真人认证 2 个 Action，共 12 个 |

早期版本放行了整个 `/api/v3/` 前缀（连带 `/api/compatible/`、`/api/coding/`），
等于把 chat/completions、embedding 等全部官方能力免费开给持站点令牌的客户 ——
那些路径不走计费链路，额度扣不到。故收缩为上表。**官方新增接口不会自动可用**，
需要显式加进白名单。

**双认证自动切换。** 站点按请求形态判断走哪套上游认证：

| 请求形态 | 上游认证 | 上游 host 来源 |
|---|---|---|
| `/api/v3/contents/generations/tasks…` | `Authorization: Bearer <渠道密钥>` | 渠道 BaseURL |
| `POST /volc/?Action=X&Version=2024-01-01` | AK/SK HMAC-SHA256 签名（Service=`ark`） | 渠道「控制面地址」或按区域推导 |

**素材按用户隔离。** 上游一个渠道只有一套 AK/SK，火山侧所有客户共用同一个资产
命名空间，官方 `ProjectName` 又是 IAM 预建概念、无法按令牌动态开。所以隔离由站点
记账：客户每创建一个素材/素材组，站点记下 `user_id + channel_id + resource_id`，
之后的读、改、删以及视频提交里的 `asset://<id>` 引用都按这张表校验，
`ListAssets` 的响应也按归属过滤后再回给客户。详见
[素材库管理接口](./asset-admin-api.md)。

**只对视频生成计费。** 仅 `POST /api/v3/contents/generations/tasks` 走计费链路，
其余接口（12 个控制面 Action、任务查询/删除）只校验站点令牌，不扣额度。

**任务查询回源官方。** 查询/删除任务时站点按 task id 反查提交渠道，用同一密钥回源官方，
返回官方权威结果，不读本地快照。

## 相关实现

| 文件 | 职责 |
|---|---|
| `router/volc-passthrough-router.go` | 路由注册：数据面挂站点根，控制面挂 `/volc` |
| `middleware/volc_passthrough.go` | 路径还原、选渠道、判定是否计费 |
| `controller/relay_passthrough.go` | 非计费接口的字节流转发与归属校验 |
| `relay/channel/volcpassthrough/endpoint.go` | 4 条数据面路由 + 12 个 Action 白名单、首部过滤 |
| `relay/channel/volcpassthrough/forward.go` | 上游 URL 解析与签名 |
| `relay/channel/volcpassthrough/adaptor.go` | 视频生成任务适配（计费链路） |
| `relay/channel/volcpassthrough/assetiso/` | 请求体/响应体解析（纯函数，无 model 依赖） |
| `service/volc_asset_isolation.go` | 归属校验、响应过滤、创建记账 |
| `model/volc_asset.go` | `volc_assets` 归属账本 |
