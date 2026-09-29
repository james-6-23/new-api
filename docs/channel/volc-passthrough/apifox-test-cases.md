# seedance-2 官方透传 — Apifox 测试用例说明

配套文件：
- `apifox-seedance2.postman_collection.json`（Postman Collection v2.1.0）
- `apifox-seedance2.openapi.json`（OpenAPI 3.0.0，已通过规范校验）

**两个文件对应 Apifox 的两个不同导入器，选错了就报"请检查是否选择了正确的数据格式及数据文件"**：

| 你的导入方式 | 用哪个文件 |
| --- | --- |
| 导入数据 → **Postman** | `apifox-seedance2.postman_collection.json` |
| 导入数据 → **OpenAPI/Swagger** | `apifox-seedance2.openapi.json` |

本文件是同一批用例的中文速查表，用于人工核对与手工回归。

## 一、导入与准备

1. Apifox → 新建项目 → 导入数据 → 按上表选对格式再上传。**用 OpenAPI/Swagger 导入器读 Postman 文件必然失败**，反之亦然。
   OpenAPI 版里控制面只有一条 `POST /volc/` —— 这和官方一致：火山控制面是 RPC 风格，12 个 Action 共用同一个 URL，靠 query 里的 `Action` 分发（站点也是按 `Action` 查白名单，不按路径分发）。`Action` 已做成**下拉枚举**（12 个带中文标签），选完动作再把请求体切到同名的命名示例，两者必须配套。Postman 版则是把 12 个 Action 摊成 12 条独立请求，便于逐条挂断言跑回归。
2. 导入后先在「环境/集合变量」里把 `baseUrl`、`token`、`cookie`、`user_id`、`channel_id` 改成真实值。
3. **不要用生产渠道跑负向用例**：`/api/v3/contents/generations/tasks` 的 POST 是会计费的，6.3 / 6.6 两条负向用例也走这个入口。建一个测试渠道（同一个 AK/SK 也行，但单独建渠道便于清理）。
4. 模型 id 分两套，不能混用：
   - 国内：`doubao-seedance-2-5-260628`、`doubao-seedance-2-0-260128`、`doubao-seedance-2-0-fast-260128`、`doubao-seedance-2-0-mini-260615`、`doubao-seedance-1-0-pro-250528`、`doubao-seedance-1-0-pro-fast-251015`、`doubao-seedance-1-5-pro-251215`（官方已标记即将下线）。
   - 海外：2.x 带 `dreamina-` 前缀（`dreamina-seedance-2-5-260628` 等），1.x **不带**前缀（`seedance-1-0-pro-250528`）。
   - 集合变量 `model` / `model_1_0` 就是为了一处切换；1.4 里的 4k 模型是写死的 `doubao-seedance-2-0-260128`，海外要手动换成 `dreamina-seedance-2-0-260128`。

## 二、集合变量

| 变量 | 初始值 | 用途 | 自动写回 |
| --- | --- | --- | --- |
| `baseUrl` | `https://your-site.com` | 站点地址，不带尾斜杠 | 否 |
| `token` | `sk-your-site-token` | 站点令牌，数据面与控制面都用它 | 否 |
| `version` | `2024-01-01` | 控制面 `Version` 参数，固定值 | 否 |
| `model` | `doubao-seedance-2-5-260628` | 2.x 系列默认模型 | 否 |
| `model_1_0` | `doubao-seedance-1-0-pro-250528` | 1.0 系列（`frames` 模式） | 否 |
| `cookie` | `session=xxxxx` | 站点登录态，仅 `/api/volc_asset/*` 用 | 否 |
| `user_id` | `7` | 站点鉴权链要求每个请求带 `New-Api-User` 头，值必须与登录身份一致 | 否 |
| `group_id` | `assetgroup-20260410113901-qk2mv` | 素材组 id | 2.1 断言通过后写入 |
| `asset_id` | `asset-20260410114236-8cdfz` | 素材 id | 3.1 断言通过后写入 |
| `task_id` | `cgt-20260410120000-abcde` | 视频任务 id | 1.1 / 1.2 / 1.3 写回 |
| `byted_token` | `byted-xxxxxxxx` | 真人认证一次性令牌 | 4.1 写回 |
| `record_id` | `88` | **站点账本行 id**（不是火山资源 id） | 否，去 5.2 列表里找 |
| `channel_id` | `12` | 透传渠道 id，`/sync` 用 | 否 |

> 写回逻辑只认 HTTP 200 且响应体能解析出目标字段，负向用例不会污染变量。

## 三、正向用例总表

### 一、视频生成（数据面，认证 `Authorization: Bearer {{token}}`）

数据面挂在站点**根**路径下，与官方 `ark.cn-beijing.volces.com` 同形。只有 POST 创建任务计费，其余 15 个入口免费。

| # | 用例 | 方法 / 路径 | 关键请求体 | 预期 | 断言 |
| --- | --- | --- | --- | --- | --- |
| 1.1 | 创建任务-文生视频【计费】 | POST `/api/v3/contents/generations/tasks` | `model` + `content:[{type:text}]` + `resolution:"720p"` + `duration:5` + `ratio:"16:9"` + `watermark:false` | 200，`{ "id": "cgt-..." }` | `id` 匹配 `/^cgt-/`，写回 `task_id` |
| 1.2 | 创建任务-首帧图生视频【计费】 | 同上 | `content` 里加 `{type:"image_url", image_url:{url:"https://..."}, role:"first_frame"}`，`ratio:"adaptive"` | 200 | 同上 |
| 1.3 | 引用自有素材【计费】 | 同上 | `image_url.url = "asset://{{asset_id}}"` | 200 | 响应文本 **不含** `resource_not_found` |
| 1.4 | 4k 分辨率【计费】 | 同上 | 模型固定 `doubao-seedance-2-0-260128`，`resolution:"4k"` | 200 | — |
| 1.5 | 1.0 系列 `frames` 模式【计费】 | 同上 | `model:"{{model_1_0}}"`，`frames:121` | 200 | — |
| 1.6 | 任务列表 | GET `/api/v3/contents/generations/tasks?page_num=1&page_size=10` | — | 200，`items[]` + `total` | — |
| 1.7 | 任务详情 | GET `/api/v3/contents/generations/tasks/{{task_id}}` | — | 200，`status` 为 `queued`/`running`/`succeeded`/`failed` | — |
| 1.8 | 取消/删除任务 | DELETE `/api/v3/contents/generations/tasks/{{task_id}}` | — | 200；已成功后删除只是清理记录 | — |

要点：
- `frames` 与 `duration` 是两套时长表达，只能给一个。1.0 系列用 `frames`（`25 + 4n`，121 → 5 秒），2.x 用 `duration` 秒数。
- 时长/分辨率矩阵：只有 2.5 能到 30 秒；4k 只有 2.0 支持；1080p 在 2.0 Fast / Mini 不支持；帧率统一 24 fps。
- 结构化字段强校验在官方侧，站点原样透传，不额外拦截。

### 二、素材组（控制面，5 个 Action）

URL 形状：`{{baseUrl}}/volc/?Action=X&Version={{version}}`。`/volc` 前缀不能省：官方控制面在宿主机根，而站点根被前端占用，所以这里单独留了一段。控制面签名由站点用渠道里的 AK/SK 现签（AWS SigV4 风格，`Service=ark`），客户只出示站点令牌。

| # | Action | 请求体 | 预期响应要点 |
| --- | --- | --- | --- |
| 2.1 | `CreateAssetGroup` | `{"Name":"我的角色库","Description":"短剧主角形象","GroupType":"AIGC"}` | `Result.Id` 匹配 `/^assetgroup-/`，写回 `group_id` |
| 2.2 | `ListAssetGroups` | `{"Filter":{"GroupType":"AIGC"},"MaxResults":20}` | `Result.Items[]`；返回**只有本人可见的组**（含 `LivenessFace`） |
| 2.3 | `GetAssetGroup` | `{"Id":"{{group_id}}"}` | `Result.Id` / `Name` / `GroupType` / `Status` |
| 2.4 | `UpdateAssetGroup` | `{"Id":"{{group_id}}","Name":"改名"}` | 200，改动生效 |
| 2.5 | `DeleteAssetGroup` | `{"Id":"{{group_id}}"}` | 200，级联删除组下素材 |

要点：
- `GroupType` 只有 `AIGC` 与 `LivenessFace`。`LivenessFace` 由真人认证流程自动创建，不要手工建。
- 单资源类 Action（Get/Update/Delete）会做归属校验：id 存在但不属于当前用户 → 按不存在处理。
- **分页陷阱**：站点是在拿到官方整页之后再做归属过滤的，所以一页里实际返回的条数可能少于 `MaxResults`，但后面还有下一页。判断有没有下一页只看 `NextToken` 是否为空，**不要**用条数是否等于 `MaxResults` 来判断。
- 深翻页阈值：素材组 `PageSize × PageNumber > 10000` 触发官方限制。

### 三、素材（控制面，5 个 Action）

| # | Action | 请求体 | 预期响应要点 |
| --- | --- | --- | --- |
| 3.1 | `CreateAsset` | `{"AssetType":"Image","URL":"https://example.com/hero-face.png","GroupId":"{{group_id}}","Name":"主角正脸"}` | `Result.Id` 匹配 `/^asset-/`，写回 `asset_id` |
| 3.2 | `ListAssets` | `{"Filter":{"GroupType":"AIGC","Statuses":["Active"]},"MaxResults":20,"SortBy":"CreateTime","SortOrder":"Desc"}` | `Result.Items[]`，每条含 `Id`/`Name`/`AssetType`/`Status`/`GroupId`/`CreateTime` |
| 3.3 | `GetAsset` | `{"Id":"{{asset_id}}"}` | 200。失败时 `Result` 为空并带 `Error.Code` |
| 3.4 | `UpdateAsset` | `{"Id":"{{asset_id}}","Name":"改名"}` | 200 |
| 3.5 | `DeleteAsset` | `{"Id":"{{asset_id}}"}` | 200 |

`GetAsset` 的 `Error.Code` 分类（判断"这个素材能不能用"的权威接口）：

| Code | 含义 | 处理建议 |
| --- | --- | --- |
| —（无 Error） | 可用 | 直接引用 |
| NotFound / 资源不存在 | id 不存在或不属于当前账号 | 重新上传 |
| Processing 未完成 | 还在处理中 | 轮询到 `Status=Active` 再用 |
| Failed | 处理失败 | 看 `Error` 详情，重新上传 |
| 内容审核未通过 | 违规 | 换素材 |

要点：
- `AssetType` 只有 `Image` / `Video` / `Audio`；`Status` 只有 `Active` / `Processing` / `Failed`。
- 上传后要**轮询到 `Active`** 才能拿去生成视频，`Processing` 阶段引用会失败。
- 深翻页阈值：素材 `PageSize × PageNumber > 20000` 触发官方限制（比素材组的 10000 宽一档）。
- 孤儿的定义：本地有行但 `user_id = 0`。未记录的定义：官方有、本地没有行。两者都在管理页处理。

### 四、真人认证（控制面，2 个 Action）

| # | Action | 请求体 | 预期响应要点 |
| --- | --- | --- | --- |
| 4.1 | `CreateVisualValidateSession` | `{"CallbackURL":"https://your-app.com/liveness/done"}` | `Result.BytedToken` / `Result.H5Link` / `Result.CallbackURL`，写回 `byted_token` |
| 4.2 | `GetVisualValidateResult` | `{"BytedToken":"{{byted_token}}"}` | `Result.GroupId`（`LivenessFace` 组的 id） |

要点：
- `BytedToken` 有效期 **30 分钟**且**只能认证一次**；过期或重复使用都要重新调 4.1。`H5Link` 同样一次性。
- `H5Link` 末尾 `lng` 参数可指定页面语言：`zh`（默认）/ `en` / `zh-Hant`。
- 回调里 `resultCode = 10000` 才算认证通过；其余是失败码，**失败时不要调 4.2**，没有组可取，重新拉一次认证。
- 算法侧子错误码看 `algorithmBaseRespCode`；`reqMeasureInfoValue`：`0` 不计费 / `1` 计费；`verify_type` 固定 `real_time`。
- 4.2 的响应可能不带 `ResponseMetadata`/`Result` 信封，官方示例是裸的 `{"GroupId":"..."}`，两层都要试。
- 站点在这一刻记下归属。之后 `ListAssetGroups(Filter.GroupType=LivenessFace)` 能看到它，别人看不到。
- `BytedToken` 是你这一侧的秘密：站点不代管也不二次校验，谁拿到谁就能换走那个组 id，别写进前端可见 URL 或日志。
- 真人认证官方当前限时免费，站点对这两个 Action 也不扣额度。

### 五、站点素材账本管理（`/api/volc_asset/`，6 条）

认证走**站点登录态**（Cookie 或 Access Token），不走火山签名。响应信封统一为 `{"success","message","data"}`。

这六条**必须同时带两个头**：`Cookie: {{cookie}}` + `New-Api-User: {{user_id}}`。`middleware.authHelper` 无条件校验 `New-Api-User`，只给 Cookie 会 401。权限不足时返回的是 **HTTP 200 + `success:false`**（`common.ApiError` 走 `http.StatusOK`），不是 403。

| # | 用例 | 方法 / 路径 | 认证 | 说明 |
| --- | --- | --- | --- | --- |
| 5.1 | 我的资产 | GET `/api/volc_asset/self?p=1&page_size=20&resource_type=asset` | UserAuth（任意登录用户） | `user_id` **只取认证上下文**，绝不接受 query 传入 |
| 5.2 | 我的额度 | GET `/api/volc_asset/self/quota` | UserAuth（任意登录用户） | 返回 `limit`/`used`/`enforced`/`count_asset_groups` |
| 5.3 | 全部资产 | GET `/api/volc_asset/?p=1&page_size=20&status=Active&keyword=asset-2026` | RootAuth（`role >= 100`） | **末尾 `/` 不能省**；可选 `user_id`/`channel_id`/`resource_type`/`status`/`resource_id`/`keyword`/`start_timestamp`/`end_timestamp` |
| 5.4 | 额度概览 | GET `/api/volc_asset/quota` | RootAuth | 每渠道一行 |
| 5.5 | 回源同步 | POST `/api/volc_asset/sync?channel_id={{channel_id}}` | RootAuth | 不传 `channel_id` 即同步全部透传渠道 |
| 5.6 | 删除资产 | DELETE `/api/volc_asset/{{record_id}}` | RootAuth | 先删官方，再删本地 |

`/quota` 每行字段：

| 字段 | 含义 |
| --- | --- |
| `quota_limit` | 站点管理员在渠道配置里**手填**的值（官方无查限额的 API），`0` = 未设置 |
| `upstream_assets` / `upstream_groups` | 官方权威计数（含孤儿），来自 `ListAssets` 的 `TotalCount` |
| `local_assets` / `local_groups` | 本站账本里的行数 |
| `orphan_assets` / `orphan_groups` | 本地 `user_id = 0` 的行数 |
| `unrecorded` | 官方有、本地无记录的粗略估算 |
| `error` | 单渠道查询失败时只在该行标记，不让整页失败 |

`/sync` 三种处置：

| 处置 | 场景 | 动作 |
| --- | --- | --- |
| `claimed` | 官方有、本地无 | 插入一行 `user_id = 0`（孤儿） |
| `refreshed` | 官方有、本地有 | 刷新 `name`/`asset_type`/`status` 快照，**归属不动** |
| `stale` | 本地有、官方无 | **只计数不删**，提示管理员去看一眼 |

要点：
- `/sync` 幂等：重复跑同一渠道，第二次 `claimed` 为 0、`refreshed` 为全量。上限 100 页 × 100 条。
- 5.6 的 `:id` 是**站点账本行 id**（`volc_assets` 主键），不是火山那边的 `asset-xxx`。去 5.3 列表里取。
- 5.5 顺序不可颠倒：先删官方，官方成功后才删本地行。失败时本地行必须留着，否则会出现"官方还在、本地无记录"的不可见占用。三种失败信息：
  - `无效的资产记录 id` — `:id` 非正整数
  - `资产所属渠道 #N 不可用` — 渠道被删或禁用，拿不到 AK/SK
  - `官方删除失败,已保留本地记录: …` — 上游失败，本地行保留，可直接重试
- 删除 `asset_group` 会级联清理本地同 `group_id` 的行。

## 四、端到端串联（跑通一条完整链路）

按顺序跑，变量会自动串起来。这是验收这次透传功能是否真正可用的最小闭环。

```
① 2.1 CreateAssetGroup   → 写回 group_id
② 3.1 CreateAsset        → 写回 asset_id
③ 3.2/3.3 轮询到 Status=Active      ← 必须等到 Active 才能引用
④ 1.3 提交生成任务（asset://{{asset_id}}） → 写回 task_id
⑤ 1.7 轮询任务详情到 status=succeeded   → 取 content.video_url
⑥ 3.5 DeleteAsset / 2.5 DeleteAssetGroup 清理
```

步骤 ⑤ 的轮询：出片通常要几十秒到几分钟，间隔 10–15 秒重试，别把 `page_num/page_size` 的分页查询当成轮询（列表接口不按时间倒序时容易看错）。任务 `status` 到 `succeeded` 后 `content.video_url` 才是最终视频地址，`failed` 时看 `error` 字段。

验证归属隔离：用**第二个**站点令牌重复 5.1 和 2.2，应该看不到第一个令牌建的组和素材。注意粒度是 `user_id` 不是 `token_id` —— 同一个用户名下的两个令牌，看到的是同一份资产。

## 五、反向用例总表（断言拦截生效）

站点自己挡下来的错误统一是这个形状，`message` 末尾会带上请求 id，方便查日志：

```json
{ "error": { "code": "endpoint_not_supported", "message": "…（request id: xxx）", "type": "new_api_error" } }
```

| # | 用例 | 请求 | 预期 | 错误码 / 断言 |
| --- | --- | --- | --- | --- |
| 6.1 | 非白名单数据面路径 | POST `/api/v3/chat/completions` | **404** | `error.code = endpoint_not_supported`，`error.type = new_api_error`，message 含「本渠道仅提供 Seedance 视频生成与素材库接口,该接口未开放」 |
| 6.2 | 非白名单控制面 Action | POST `/volc/?Action=ListEndpoints&Version=...` | **404** | 同 6.1，文案一致（站点不伪装成官方错误，客户能立刻分清是站点挡的还是火山挡的） |
| 6.3 | 跨账号 `asset://` 引用 | POST 创建任务，url 用别人的素材 id | **404** | `error.code = resource_not_found`，message 含「素材不存在或不属于当前账号: 」后面列出被拒的 id |
| 6.4 | 占位/失效令牌 | GET 任务列表，Bearer `sk-invalid-placeholder` | **401** | 令牌无效/禁用/过期 → 401；分组不可用或额度不足 → 403 |
| 6.5 | 缺 Action 的 `/volc` 请求 | POST `/volc/foo/bar` | **400** | `error.code = invalid_path` |
| 6.6 | 1080p 用于 2.0 Fast | POST 创建任务，`model=doubao-seedance-2-0-fast-260128`，`resolution=1080p` | 非 200 | 官方结构化字段强校验拒绝，站点不额外拦 |

补充建议（按需手加，集合里未内置）：

| 用例 | 请求 | 预期 |
| --- | --- | --- |
| 4k 用于非 2.0 模型 | 1.0 或 2.0 Fast 上给 `resolution=4k` | 官方拒绝 |
| `frames` 用在非 1.0 模型 | `model={{model}}` + `frames:121` | 官方拒绝 |
| 同时给 `frames` 和 `duration` | 两者都给 | 官方拒绝 |
| 深翻页越界 | 素材组 `PageSize × PageNumber > 10000` | 官方拒绝 |
| 未配置透传渠道 | 用一个所在分组下没有可用 type 60 渠道的令牌请求 | 分组下没有可用的字节火山透传渠道 |
| 渠道配置缺失 | 渠道 AK/SK 为空 | **500** `channel_config_error` |
| 任务查询回错渠道 | 用渠道 B 的令牌查渠道 A 提交的 `task_id` | 按 `task_id` 反查提交渠道；提交渠道被禁用时提示「提交渠道 #N 已被禁用」 |
| 上游不可达 | 渠道 `BaseURL` 指向不可达地址 | **502** `do_request_failed` |
| 官方返回非 JSON | 上游返回 HTML 错误页 | **500** `asset_list_filter_failed`（列表过滤阶段解析失败） |
| 归属校验内部失败 | 构造本地行读失败 | **500** `asset_ownership_check_failed` |
| `/api/volc_asset/self` 传 `user_id` | 手工加 `&user_id=1` | 被忽略，仍只返回自己的 |
| 非超管访问 5.3/5.4/5.5/5.6 | 用普通用户或普通管理员登录态 | **HTTP 200 + `success:false`**，`无权进行此操作，权限不足`（不是 403） |
| 无登录态访问 5.1 | 不带 Cookie | **401** `无权进行此操作，未登录且未提供 access token` |
| 漏带 `New-Api-User` | 5.1~5.5 任一条去掉该头 | **401** `无权进行此操作，未提供 New-Api-User` |
| `New-Api-User` 与登录身份不符 | 把 `{{user_id}}` 改成别人的 id | **401** `无权进行此操作，New-Api-User 与登录用户不匹配` |

> `/api/volc_asset/*` 的头一共两个：`Cookie: {{cookie}}` 和 `New-Api-User: {{user_id}}`。少一个就是 401。`New-Api-User` 值也可以换成 "用站点 Access Token 走 `Authorization` 头" 的方式，但两个头仍需同时存在。

## 六、已知差异（测试时以行为准）

`/api/volc_asset/` 、`/quota`、`/sync`、`/:id` DELETE 四条挂的是 `RootAuth()`（`role >= 100`），两个主题的前端门槛也是超管，测这四条必须用超管账号。`/self` 与 `/self/quota` 是 `UserAuth()`，普通用户即可。

素材条数上限官方**没有查询接口**，真实值在火山控制台 → 配额管理 里看、按主账号计、按 配额中心 → 申请配额 提额，所以站点这边的数字是手工配的（系统设置 → 运营设置 → 素材库额度，单用户提额在用户管理里覆盖）。开启强制后超量创建会在转发前被拦，返回 `403` `asset_limit_exceeded`。

## 七、计费口径（避免误判）

16 个入口里**只有** `POST /api/v3/contents/generations/tasks` 计费，其余 15 个（列表、详情、取消、10 个素材 Action、2 个真人认证 Action）全部免费。分派逻辑：命中计费键 → `controller.RelayTask`，否则 → `controller.RelayVolcPassthrough`。

所以回归时可以放心刷 1.6/1.7/1.8 和二、三、四各节的用例，只有 1.1–1.5 与 6.3/6.6 会真花钱。

