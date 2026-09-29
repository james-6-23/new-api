# 国内（火山方舟）· 客户端调用文档

本站点透传火山方舟的 **Seedance 视频生成**与**私域素材库**。
放行范围内的请求与响应，其路径、query、body、状态码、错误结构均与官方一致，
**照抄官方文档即可**。

## 一、只改两处

| | 官方直连 | 走本站点 |
|---|---|---|
| 数据面前缀 | `https://ark.cn-beijing.volces.com` | `https://<站点域名>` |
| 控制面前缀 | `https://ark.cn-beijing.volcengineapi.com` | `https://<站点域名>/volc` |
| 认证 | 方舟 API Key / AK·SK 签名 | `Authorization: Bearer sk-<站点令牌>` |

**数据面路径与官方逐字一致**，只换域名：

```
官方  POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks
本站  POST https://your-site.com/api/v3/contents/generations/tasks
```

控制面官方挂在域名根路径上（`https://<host>/?Action=...`），而站点根被前端页面占用，
因此控制面**保留 `/volc` 前缀**：

```
官方  POST https://ark.cn-beijing.volcengineapi.com/?Action=ListAssets&Version=2024-01-01   (AK/SK 签名)
本站  POST https://your-site.com/volc/?Action=ListAssets&Version=2024-01-01                  (站点令牌)
```

> `/volc/api/v3/...` 也仍然可用（等价写法），便于把整站统一到单一前缀。

**你不需要持有任何火山凭据**，也不需要实现 AK/SK 签名 —— 控制面签名由站点用托管的
AK/SK 完成。

## 二、开放的接口

只有下面 **16 个入口**可用。**白名单是精确匹配，不是前缀通配**，
其余官方接口（`/api/v3/chat/completions`、`/api/v3/embeddings`、`/api/compatible/`、
`Endpoint*` / `Model*` 等约 100 个控制面 Action）一律返回
`404 endpoint_not_supported`。官方新增接口不会自动可用。

| # | 接口 | 章节 |
|---|---|---|
| 1 | `POST /api/v3/contents/generations/tasks` — 创建视频任务（**唯一计费接口**） | §五 |
| 2 | `GET /api/v3/contents/generations/tasks` — 任务列表 | §五 |
| 3 | `GET /api/v3/contents/generations/tasks/{id}` — 任务详情 | §五 |
| 4 | `DELETE /api/v3/contents/generations/tasks/{id}` — 取消/删除任务 | §五 |
| 5-9 | `CreateAssetGroup` / `GetAssetGroup` / `ListAssetGroups` / `UpdateAssetGroup` / `DeleteAssetGroup` | §六 |
| 10-14 | `CreateAsset` / `GetAsset` / `ListAssets` / `UpdateAsset` / `DeleteAsset` | §七 |
| 15-16 | `CreateVisualValidateSession` / `GetVisualValidateResult` | §八 |

## 三、准备工作

下文所有测试用例均可直接复制执行，先设置两个变量：

```bash
export SITE=https://your-site.com
export TOKEN=sk-your-site-token
```

用官方 SDK 时只改 base_url 与 api_key：

```python
from volcenginesdkarkruntime import Ark
client = Ark(
    base_url="https://your-site.com/api/v3",
    api_key="sk-your-site-token",
)
```

SDK 里除视频生成之外的方法（chat、embeddings 等）会返回
`404 endpoint_not_supported` —— 本渠道未开放那些能力。

## 四、计费与隔离

**仅 `POST /api/v3/contents/generations/tasks` 计费。** 提交时按模型、分辨率档位、
是否含视频输入预扣，任务完成后按官方 `usage.total_tokens` 重算，多退少补。
任务查询/列表/删除与全部 12 个素材 Action 只校验站点令牌，**不扣额度**。

**素材按账号隔离。** 上游一个渠道只有一套凭据，火山侧所有客户共用同一个资产命名空间，
所以隔离由站点记账完成：

- 你创建的素材组与素材，站点记下归属，只有你能读、改、删；
- `ListAssetGroups` / `ListAssets` 的响应**已按归属过滤**，你只会看到自己的条目，
  `TotalCount` 也是过滤后的数字；
- 视频提交里 `asset://<id>` 引用的素材必须属于你，否则整个请求被拒；
- 别人的资产 id 对你返回 `404 resource_not_found`。**不存在与不属于你返回同一个错误**，
  不提供「这个 id 是否存在」的探测能力。

归属粒度是**账号**而不是令牌：轮换令牌不会丢素材，换账号才隔离。

### 错误处理

上游返回的状态码与错误体**原样透传**，按官方文档处理即可。
仅当站点自身出错时才返回站点结构（`"type": "new_api_error"`）：

| HTTP | code | 含义 |
|---|---|---|
| 401/403 | — | 站点令牌无效、额度不足或无权限 |
| 404 | `endpoint_not_supported` | 接口不在上面那 16 个之内 |
| 404 | `resource_not_found` | 资源不存在或不属于当前账号 |
| 400 | `invalid_path` | 路径形态不合法 |
| 500 | `channel_config_error` | 站点渠道未配置或配置不全 |
| 500 | `asset_ownership_check_failed` | 站点归属校验失败（数据库异常），请重试 |
| 502 | `do_request_failed` | 站点连不上火山上游 |
| 503 | — | 分组下无可用透传渠道 |

### 通用验证

先跑这条确认链路通（任务列表，不计费）：

```bash
curl "$SITE/api/v3/contents/generations/tasks?page_num=1&page_size=1" \
  -H "Authorization: Bearer $TOKEN"
```

返回官方格式的 `{"items": [...], "total": N}` 即表示透传链路正常。

---

## 五、视频生成（数据面 4 条）

### 5.1 `POST /api/v3/contents/generations/tasks` — 创建任务

**唯一计费接口。** `content` 数组里第一个 `text` 元素是提示词，
`image_url` / `video_url` 元素是参考媒体（首帧图、参考视频等）。

```bash
curl -X POST "$SITE/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2-5-260628",
    "content": [
      { "type": "text", "text": "女孩抱着狐狸，女孩睁开眼，温柔地看向镜头，镜头缓缓拉出，女孩的头发被风吹动" },
      { "type": "image_url", "image_url": { "url": "https://example.com/first-frame.png" } }
    ],
    "resolution": "1080p",
    "ratio": "adaptive",
    "duration": 5,
    "generate_audio": true,
    "watermark": false
  }'
```

```json
{ "id": "cgt-20260410120000-abcde" }
```

拿到 `id` 后按 §5.3 轮询。

#### 参数

| 参数 | 取值 | 说明 |
|---|---|---|
| `model` | 见下方模型表 | 必填 |
| `content` | 数组 | 必填。`text` / `image_url` / `video_url` 元素 |
| `resolution` | `480p` `720p` `1080p` `4k` | 全小写。`1080p` 在 2.0 Fast / 2.0 Mini 暂不支持；`4k` 仅 Seedance 2.0 支持 |
| `ratio` | `16:9` `4:3` `1:1` `3:4` `9:16` `21:9` `adaptive` | `adaptive` 按输入媒体自适应 |
| `duration` | 秒数，或 `-1` | `-1` = 智能指定时长，仅 2.5 / 2.0 系列 / 1.5 pro 支持。1.0 系列取 `[2,12]` 且**无** `-1` |
| `frames` | `[29,289]`，须满足 `25+4n` | **仅 Seedance 1.0 系列**。与 `duration` 同时传时 `frames` 优先 |
| `generate_audio` | `true` / `false` | 是否生成音频 |
| `watermark` | `true` / `false` | 是否打水印 |
| `output_format` | `mp4`（2.5 另支持 `mov`） | 输出容器 |

> **结构化字段是推荐写法。** 官方另有把参数拼在提示词后的旧写法
> （`--resolution 1080p` 这类后缀），官方标为「旧方式，弱校验」——
> 参数写错会被忽略或报错。结构化字段是「常规方式（推荐），强校验」，
> 填错会明确返回错误提示。**没有任何模型必须用后缀写法。**

#### 模型

```
doubao-seedance-2-5-260628
doubao-seedance-2-0-260128
doubao-seedance-2-0-fast-260128
doubao-seedance-2-0-mini-260615
doubao-seedance-1-0-pro-250528
doubao-seedance-1-0-pro-fast-251015
```

每个模型都另有一个不带日期的别名（如 `doubao-seedance-2-5`），
但官方全部请求示例用的是带日期的形式，**建议照用带日期的 id**。
具体可用模型以站点渠道的模型列表为准，不在列表内会被拒。

> `doubao-seedance-1-5-pro-251215` 官方标记**即将下线**，不要用于新接入。

### 5.2 `GET /api/v3/contents/generations/tasks` — 任务列表

```bash
curl "$SITE/api/v3/contents/generations/tasks?page_num=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 5.3 `GET /api/v3/contents/generations/tasks/{id}` — 任务详情

**回源官方**，返回的是官方权威状态，不是站点快照。

```bash
curl "$SITE/api/v3/contents/generations/tasks/cgt-20260410120000-abcde" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "id": "cgt-20260410120000-abcde",
  "model": "doubao-seedance-2-5-260628",
  "status": "succeeded",
  "content": { "video_url": "https://...mp4" },
  "usage": { "total_tokens": 128000 }
}
```

`status` 走 `queued` → `running` → `succeeded` / `failed` / `cancelled`。
`succeeded` 后 `content.video_url` 是**有时效的下载地址**，及时转存。

### 5.4 `DELETE /api/v3/contents/generations/tasks/{id}` — 取消/删除

```bash
curl -X DELETE "$SITE/api/v3/contents/generations/tasks/cgt-20260410120000-abcde" \
  -H "Authorization: Bearer $TOKEN"
```

排队中的任务为取消，已完成的为删除记录。**已扣费不因删除而退回**
（未完成任务取消后按官方实际用量结算）。

---

## 六、素材组（控制面 5 个 Action）

控制面统一形态：`POST $SITE/volc/?Action=<X>&Version=2024-01-01`，body 为 JSON，
字段名**首字母大写**（与数据面的小写下划线风格不同，这是官方的区分，照抄即可）。

所有 Action 都接受 `ProjectName`（默认 `default`）。**通常不需要填** ——
站点渠道已配好项目名，填错反而查不到资源。

> 首次创建素材组前需在火山控制台签署授权函，否则官方直接拒绝。
> 这一步由站点运营方完成。

### 6.1 `CreateAssetGroup` — 创建素材组

| 参数 | 类型 | 必选 | 说明 |
|---|---|---|---|
| `Name` | string | 是 | 上限 64 字符 |
| `Description` | string | 否 | 上限 300 字符 |
| `GroupType` | string | 否 | **仅支持 `AIGC`**（虚拟人像） |

```bash
curl -X POST "$SITE/volc/?Action=CreateAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Name": "我的角色库", "Description": "短剧主角形象", "GroupType": "AIGC" }'
```

```json
{ "ResponseMetadata": { "Action": "CreateAssetGroup" },
  "Result": { "Id": "assetgroup-20260410113901-qk2mv" } }
```

返回的 `Id` 就是后续 `CreateAsset` 要填的 `GroupId`。**站点在这一刻记下归属**，
之后只有你能操作这个组。

> 真人素材组（`LivenessFace`）**不能自建**，走 §八 的真人认证流程由官方分配。

### 6.2 `ListAssetGroups` — 素材组列表

`Filter` 必选，且 **`Filter.GroupType` 必选**。

| 参数 | 类型 | 说明 |
|---|---|---|
| `Filter.GroupType` | string | **必选**。`AIGC` \| `LivenessFace` |
| `Filter.GroupIds` | string[] | 按 id 批量查 |
| `Filter.Name` | string | 按名称查，上限 64 字符 |
| `NextToken` / `MaxResults` | string / int | 游标分页，`MaxResults` 取 `[1,100]`，默认 10 |
| `PageNumber` / `PageSize` | int / int | 页码分页（兼容写法） |
| `SortBy` | string | 默认 `CreateTime` |

**两种分页方式只能选一种**，同时传会被官方拒绝。不传任何分页参数时默认走
游标分页、`MaxResults = 10`。深翻页（`PageSize * PageNumber > 10000`）必须用 `NextToken`。

```bash
curl -X POST "$SITE/volc/?Action=ListAssetGroups&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Filter": { "GroupType": "AIGC" }, "MaxResults": 20 }'
```

**响应已按归属过滤**：`Items` 里只有你自己的组，`TotalCount` 也是过滤后的数字。
因此在你这一侧，官方的 `NextToken` 翻页可能出现某一页条目数少于 `MaxResults`
却仍有下一页的情况 —— **以 `NextToken` 是否为空判断是否还有下一页**，不要靠条目数推断。

### 6.3 `GetAssetGroup` — 素材组详情

| 参数 | 类型 | 必选 |
|---|---|---|
| `Id` | string | 是 |

```bash
curl -X POST "$SITE/volc/?Action=GetAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "assetgroup-20260410113901-qk2mv" }'
```

`Result` 字段：`Id`、`Name`、`Description`、`GroupType`、`ProjectName`、
`CreateTime`、`UpdateTime`。

### 6.4 `UpdateAssetGroup` — 更新素材组

`Id` 必选，`Name`（≤64）与 `Description`（≤300）按需传。

```bash
curl -X POST "$SITE/volc/?Action=UpdateAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "assetgroup-20260410113901-qk2mv", "Name": "主角库（第二季）" }'
```

### 6.5 `DeleteAssetGroup` — 删除素材组

`Id` 必选。成功时 HTTP 200，`Result` 为空对象 `{}`。

```bash
curl -X POST "$SITE/volc/?Action=DeleteAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "assetgroup-20260410113901-qk2mv" }'
```

> **删除素材组会连带删除组内所有素材。** 组内素材的账本记录同时清理。
> 这一步不可逆，删组之前先确认组里没有还要用的素材。

---

## 七、素材（控制面 5 个 Action）

### 7.1 `CreateAsset` — 上传素材

| 参数 | 类型 | 说明 |
|---|---|---|
| `AssetType` | string | `Image` \| `Video` \| `Audio` |
| `URL` | string | 素材的**公网可访问地址** |
| `GroupId` | string | 所属素材组 id（§6.1 返回的那个） |
| `Name` | string | ≤64 字符 |

**只接受 URL，不接受 Base64。** 站点不代传文件，URL 必须是火山侧能直接下载的地址。

> `Name` **只用于 `ListAssets` 的模糊搜索，不会进入模型推理**。
> 想让画面里出现什么，写在视频提交的提示词里，不要指望素材名生效。

```bash
curl -X POST "$SITE/volc/?Action=CreateAsset&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "AssetType": "Image",
    "URL": "https://example.com/hero-face.png",
    "GroupId": "assetgroup-20260410113901-qk2mv",
    "Name": "主角正脸"
  }'
```

```json
{ "ResponseMetadata": { "Action": "CreateAsset" },
  "Result": { "Id": "asset-20260410114236-8cdfz" } }
```

`GroupId` 必须是你自己的组，否则 `404 resource_not_found`。

#### 素材规格限制

| | 图像 | 视频 | 音频 |
|---|---|---|---|
| 格式 | jpeg png webp bmp tiff gif heic heif | mp4 mov | wav mp3 |
| 大小 | < 30 MB | ≤ 200 MB | ≤ 15 MB |
| 时长 | — | [2, 30] s | [2, 30] s |
| 宽高比 | (0.4, 2.5) | [0.4, 2.5] | — |
| 边长 px | (300, 6000) | [300, 6000] | — |
| 总像素 | — | [407696, 8295044] | — |
| 帧率 | — | [24, 60] FPS | — |
| 分辨率 | — | 480p / 720p / 1080p / 4K | — |

不同模型能吃的音视频时长不同 —— **上传时记下时长**，否则后面分不清哪些素材
能用于 Seedance 2.0、哪些能用于 2.5。

上传后素材进入 `Processing`，转码完成才变 `Active`。**`Processing` 状态不能用
`asset://` 引用**，需先按 §7.3 轮询到 `Active`。

### 7.2 `ListAssets` — 素材列表

`Filter` 必选，`Filter.GroupType` 必选。

| 参数 | 类型 | 说明 |
|---|---|---|
| `Filter.GroupType` | string | **必选**。`AIGC` \| `LivenessFace` |
| `Filter.GroupIds` | string[] | 限定素材组 |
| `Filter.Name` | string | 名称模糊搜索，≤64 字符 |
| `Filter.Statuses` | string[] | `Active` \| `Processing` \| `Failed` |
| `NextToken` / `MaxResults` | string / int | 游标分页，`[1,100]`，默认 10 |
| `PageNumber` / `PageSize` | int / int | 页码分页（兼容写法） |
| `SortBy` | string | `CreateTime`（默认）\| `UpdateTime` \| `GroupId` |
| `SortOrder` | string | `Desc`（默认）\| `Asc` |

分页规则同 §6.2：两种方式互斥；游标分页**不返回** `TotalCount` / `PageNumber` /
`PageSize`，**`NextToken` 缺失即遍历结束**；翻页时 `Filter` / `SortBy` / `SortOrder`
应与首次查询保持一致。深翻页阈值这里是 `PageSize * PageNumber > 20000`。

```bash
curl -X POST "$SITE/volc/?Action=ListAssets&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "Filter": { "GroupType": "AIGC", "Statuses": ["Active"] },
    "MaxResults": 20, "SortBy": "CreateTime", "SortOrder": "Desc"
  }'
```

**响应已按归属过滤，只含你自己的素材。**

### 7.3 `GetAsset` — 素材详情

`Id` 必选。这是**判断素材能否使用的权威接口**。

```bash
curl -X POST "$SITE/volc/?Action=GetAsset&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "asset-20260410114236-8cdfz" }'
```

`Result` 字段：`Id`、`Name`、`AssetType`、`GroupId`、`Status`、`ProjectName`、
`CreateTime`、`UpdateTime`，失败时另有 `Error.Code` / `Error.Message`。

`Status` = `Active`（可用）/ `Processing`（预处理中，不可用）/ `Failed`（处理失败）。

#### `Failed` 的 `Error.Code`

| 类别 | Code |
|---|---|
| 下载与格式 | `DownloadFailed` `TypeMismatch` `FormatUnsupported` `FormatUndetectable` |
| 规格越界 | `DurationTooShort` `DurationTooLong` `WidthTooSmall` `HeightTooSmall` `WidthTooLarge` `HeightTooLarge` `AspectRatioTooSmall` `AspectRatioTooLarge` `FileSizeTooLarge` `PixelCountTooSmall` `PixelCountTooLarge` `FpsTooLow` `FpsTooHigh` |
| 音轨 | `AudioTrackRequired` `AudioTrackForbidden` |
| 审核 | `ContentRestricted` `InputTextSensitiveContentDetected` `InputImageSensitiveContentDetected` `InputVideoSensitiveContentDetected` `InputAudioSensitiveContentDetected` |
| 真人一致性 | `FaceMismatch` |
| 其他 | `TranscodingFailed` |

规格类错误对照 §7.1 的限制表改文件重传；审核类与 `FaceMismatch` 换素材。
`Failed` 的素材**不会自动重试**，需删掉重新 `CreateAsset`。

### 7.4 `UpdateAsset` — 重命名素材

`Id` 必选，`Name` ≤64 字符。**只能改名**，改不了 URL 与所属组。

```bash
curl -X POST "$SITE/volc/?Action=UpdateAsset&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "asset-20260410114236-8cdfz", "Name": "主角正脸-v2" }'
```

### 7.5 `DeleteAsset` — 删除素材

`Id` 必选。成功时 HTTP 200，`Result` 为 `{}`，站点同时清理账本记录。

```bash
curl -X POST "$SITE/volc/?Action=DeleteAsset&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "Id": "asset-20260410114236-8cdfz" }'
```

---

## 八、真人认证（控制面 2 个 Action）

用于**真人 IP 视频**：真人素材组（`GroupType = LivenessFace`）不能用
`CreateAssetGroup` 自建，必须让**本人**过一次活体认证，由官方创建并返回组 id。

流程是两步一跳转：

```
CreateVisualValidateSession  →  拿到 H5Link + BytedToken
        ↓ 把 H5Link 交给终端用户，在手机上完成人脸认证
        ↓ 用户点「完成」，浏览器跳到 CallbackURL?bytedToken=…&resultCode=10000
GetVisualValidateResult      →  用 BytedToken 换回 GroupId
```

> **`BytedToken` 有效期 30 分钟，且只能认证一次。** 过期或重复使用都要重新
> `CreateVisualValidateSession`。`H5Link` 同样是一次性的，用过即失效。

真人认证服务官方**当前限时免费**（回调里的 `reqMeasureInfoValue` 为 `0` 即不计费）。
本站点对这两个 Action 也不扣额度。

### 8.1 `CreateVisualValidateSession` — 拉起认证 H5

| 参数 | 类型 | 必选 | 说明 |
|---|---|---|---|
| `CallbackURL` | string | 是 | 认证结束后跳转的**公网可访问** URL |

```bash
curl -X POST "$SITE/volc/?Action=CreateVisualValidateSession&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "CallbackURL": "https://your-app.com/liveness/done" }'
```

```json
{ "ResponseMetadata": { "Action": "CreateVisualValidateSession" },
  "Result": {
    "BytedToken": "byted-xxxxxxxx",
    "CallbackURL": "https://your-app.com/liveness/done",
    "H5Link": "https://.../liveness?token=...&lng=zh"
  } }
```

`H5Link` 末尾的 `lng` 可指定页面语言：`zh`（默认）/ `en` / `zh-Hant`。

用户点「完成」后浏览器跳转到：

```
https://your-app.com/liveness/done?bytedToken=byted-xxxxxxxx&resultCode=10000&algorithmBaseRespCode=0&reqMeasureInfoValue=1&verify_type=real_time
```

| 回调参数 | 含义 |
|---|---|
| `bytedToken` | 换 `GroupId` 用的凭证，与响应里的 `BytedToken` 一致 |
| `resultCode` | **`10000` 才是认证通过**，其余为失败码 |
| `algorithmBaseRespCode` | 服务端子错误码，`resultCode` 为服务端错误时再看它 |
| `reqMeasureInfoValue` | `0` 不计费 / `1` 计费 |
| `verify_type` | 固定 `real_time` |

**`resultCode != 10000` 时不要调 §8.2** —— 没有组可取，重新拉一次认证。

> `BytedToken` 是**你这一侧的秘密**。站点不代管它，也不做二次校验：
> 谁拿到 token 谁就能换走那个组 id，别把它写进前端可见的 URL 或日志。

### 8.2 `GetVisualValidateResult` — 换取真人素材组 id

| 参数 | 类型 | 必选 | 说明 |
|---|---|---|---|
| `BytedToken` | string | 是 | §8.1 返回的凭证 |

```bash
curl -X POST "$SITE/volc/?Action=GetVisualValidateResult&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{ "BytedToken": "byted-xxxxxxxx" }'
```

```json
{ "GroupId": "assetgroup-20260410120500-real1" }
```

> 这个 Action 的响应**可能不带 `ResponseMetadata` / `Result` 信封**，
> 官方示例是裸的 `{"GroupId": "..."}`。站点两层都兼容，解析时也请两层都试。

**站点在这一刻记下归属** —— 换回来的真人组归你，之后 `ListAssetGroups`
（`Filter.GroupType = LivenessFace`）能看到它，别人看不到。

`CreateVisualValidateSession` 返回的 `BytedToken` **不是资源，不记账**，
它 30 分钟后就没了。

拿到真人组之后，往组里加素材、查素材、删素材都走 §七的普通 Action，
`GroupId` 填这个真人组 id 即可。

---

## 九、完整工作流串联

从零到一条用自己素材生成的视频，四步：

```
① CreateAssetGroup   → GroupId
② CreateAsset        → AssetId      （轮询到 Active）
③ POST .../tasks     → TaskId       （content 里用 asset://AssetId）
④ GET  .../tasks/{id}→ video_url    （轮询到 succeeded）
```

```bash
export SITE="https://your-site.com"
export TOKEN="sk-xxxxxxxx"
V="Version=2024-01-01"

# ① 建组
GROUP=$(curl -s -X POST "$SITE/volc/?Action=CreateAssetGroup&$V" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"Name":"我的素材库","GroupType":"AIGC"}' | jq -r '.Result.Id')

# ② 传素材
ASSET=$(curl -s -X POST "$SITE/volc/?Action=CreateAsset&$V" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"AssetType\":\"Image\",\"GroupId\":\"$GROUP\",
       \"Name\":\"主角正脸\",\"URL\":\"https://example.com/hero.png\"}" \
  | jq -r '.Result.Id')

# 等转码：Processing → Active
until [ "$(curl -s -X POST "$SITE/volc/?Action=GetAsset&$V" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"Id\":\"$ASSET\"}" | jq -r '.Result.Status')" = "Active" ]; do sleep 3; done

# ③ 提交视频任务，用 asset:// 引用自己的素材
TASK=$(curl -s -X POST "$SITE/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"model\":\"doubao-seedance-2-5-260628\",
       \"content\":[
         {\"type\":\"text\",\"text\":\"女孩睁开眼看向镜头，镜头缓缓拉出\"},
         {\"type\":\"image_url\",\"image_url\":{\"url\":\"asset://$ASSET\"}}],
       \"resolution\":\"1080p\",\"ratio\":\"adaptive\",\"duration\":5}" \
  | jq -r '.id')

# ④ 轮询取片
until [ "$(curl -s "$SITE/api/v3/contents/generations/tasks/$TASK" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.status')" = "succeeded" ]; do sleep 5; done
curl -s "$SITE/api/v3/contents/generations/tasks/$TASK" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.content.video_url'
```

四个**实践要点**：

1. **`Processing` 的素材不能用。** 第 ② 步不等 `Active` 就提交，任务会因素材
   不可用失败，而失败的视频任务**照样占用预扣额度**直到结算。
2. **`asset://<id>` 只能引用自己的素材。** 引用别人的 id 一律 `404
   resource_not_found`，不会告诉你那个 id 是否真的存在。
3. **轮询别太密。** 视频任务通常几十秒到几分钟，`sleep 5` 是合适的量级；
   任务详情接口回源官方，打太狠会碰到官方限流。
4. **`video_url` 有时效。** 拿到就转存到自己的存储，别把它当长期地址存库。

真人 IP 视频把第 ① 步换成 §八 的两个 Action（认证 → 换 `GroupId`），
后面 ②③④ 完全一样。

---

## 十、分辨率与时长支持矩阵

官方按模型给能力，站点不做额外限制 —— 下表就是你能填的上限。

| 模型 | 分辨率 | 时长 | 输出格式 |
|---|---|---|---|
| `doubao-seedance-2-5-260628` | 480p / 720p / 1080p | 4~30 s | mp4, mov |
| `doubao-seedance-2-0-260128` | 480p / 720p / 1080p / **4k** | 4~15 s | mp4 |
| `doubao-seedance-2-0-fast-260128` | 480p / 720p | 4~15 s | mp4 |
| `doubao-seedance-2-0-mini-260615` | 480p / 720p | 4~15 s | mp4 |
| `doubao-seedance-1-5-pro-251215` `即将下线` | 480p / 720p / 1080p | 4~12 s | mp4 |
| `doubao-seedance-1-0-pro-250528` | 480p / 720p / 1080p | 2~12 s | mp4 |
| `doubao-seedance-1-0-pro-fast-251015` | 480p / 720p / 1080p | 2~12 s | mp4 |

帧率**七个模型统一 24 fps**。

读这张表时注意三件事：

- **`4k` 只有 `doubao-seedance-2-0-260128` 一家支持**，且 4k 的限流单独更严
  （官方标注最大并发 1）。批量跑 4k 要自己控节奏。
- **`1080p` 在 2.0 Fast 和 2.0 Mini 上不支持** —— 这两个是快/小规格，
  填 1080p 会被拒。
- **只有 2.5 能出 30 秒**，其余 2.0 系列封顶 15 秒、1.x 系列封顶 12 秒。
  需要长片就得分段生成再拼。

时长参数的两种写法（`duration` 与 1.0 系列专属的 `frames`）见 §5.1 的参数表；
`duration: -1`（智能时长）**只有 2.5 / 2.0 系列 / 1.5 pro 支持**。

---

## 十一、相关文档

| 文档 | 内容 |
|---|---|
| `asset-admin-api.md` | 站点侧素材账本的 5 个管理接口（本人资产 / 全站资产 / 额度 / 同步 / 删除） |
| `cn-channel-setup.md` | 运营方如何配置国内渠道 |
| `overseas-client-api.md` | 海外（BytePlus）客户端调用，模型 id 为 `dreamina-*` |
