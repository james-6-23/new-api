# 素材库管理接口（站点管理 API）

面向**站点前端与站点管理员**，不是给透传客户用的。

与 `/api/v3/...`、`/volc/?Action=` 两条透传链路无关：这些接口读的是站点自己的
**归属账本**（表 `volc_assets`），认证用站点的登录会话或 Access Token，不走火山签名。

| | 透传接口 | 本文接口 |
|---|---|---|
| 前缀 | `/api/v3/`、`/volc/` | `/api/volc_asset/` |
| 认证 | 站点令牌 `Authorization: Bearer sk-xxx` | 站点用户会话 / Access Token |
| 数据来源 | 回源火山官方 | 站点账本（额度与同步除外） |
| 计费 | 仅视频提交计费 | 全部不计费 |

## 一、为什么有账本

上游一个渠道只有一套 AK/SK，所有站点令牌在火山侧共用同一个资产命名空间，
官方 `ProjectName` 又是 IAM 预建概念、无法按令牌动态开。所以**隔离只能由站点记账**：
客户每创建一个素材/素材组，站点记下 `user_id + channel_id + resource_id`，
之后的读、改、删、以及视频提交里的 `asset://<id>` 引用都按这张表校验。

归属粒度是 **`user_id`**，不是 `token_id` —— 同一用户轮换令牌不该丢素材，跨用户才隔离。

## 二、认证与权限

| 路径 | 中间件 | 实际门槛 |
|---|---|---|
| `GET /api/volc_asset/self` | `UserAuth()` | 任意已登录用户 |
| `GET /api/volc_asset/self/quota` | `UserAuth()` | 同上 |
| `GET /api/volc_asset/` | `RootAuth()` | `role >= 100`（超级管理员） |
| `GET /api/volc_asset/quota` | `RootAuth()` | 同上 |
| `POST /api/volc_asset/sync` | `RootAuth()` | 同上 |
| `DELETE /api/volc_asset/:id` | `RootAuth()` | 同上 |

> 这四条走超管而非管理员：它们能看到全站客户的素材清单、能直接删除上游资源，
> 授权面比普通后台管理宽。前端门槛与后端一致（`isRoot()` / `ROLE.SUPER_ADMIN`），
> 普通用户与管理员只看得到「我的资产」。

`/self` 的 `user_id` **只取自认证上下文，不接受 query 覆盖** —— 否则任何登录用户
都能翻别人的账本。

## 三、通用约定

所有响应都是站点统一信封：

```json
{ "success": true, "message": "", "data": {} }
```

失败时 `success: false`，`message` 为原因，`data` 缺省。

列表类接口的 `data` 是分页对象：

```json
{ "page": 1, "page_size": 20, "total": 137, "items": [] }
```

分页 query 参数为 `p`（页码，从 1 开始）与 `page_size`（**上限 100**，超出按 100 处理）。

资产行结构：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | int | 账本行 id，删除接口用的就是它，**不是**火山的资产 id |
| `user_id` | int | 归属用户；`0` 表示孤儿（上游有、创建时未记上账） |
| `username` | string | 仅全量视图返回；用户已删除时为空 |
| `channel_id` | int | 所属透传渠道 |
| `resource_type` | string | `asset` \| `asset_group` |
| `resource_id` | string | 火山官方 id，如 `asset-20260410114236-8cdfz` |
| `group_id` | string | 所属素材组（仅 `asset` 行有值） |
| `name` | string | 快照，非权威 |
| `asset_type` | string | 快照：`Image` \| `Video` \| `Audio` |
| `status` | string | 快照：`Active` \| `Processing` \| `Failed` |
| `created_at` | int64 | 秒级时间戳 |
| `updated_at` | int64 | 秒级时间戳 |

> `name` / `asset_type` / `status` 三列是官方响应的**快照**，只为页面展示与搜索。
> 权威值永远回源官方（`GetAsset` / `ListAssets`）。快照在归属校验顺带解析响应时刷新，
> 不产生额外的上游调用。

## 四、接口

下文示例用站点管理端的会话 Cookie；用 Access Token 时把 `-b` 换成
`-H "Authorization: <access_token>"`。

```bash
export SITE=https://your-site.com
export COOKIE=session=xxxxx
```

### 4.1 `GET /api/volc_asset/self` — 我的资产

任意已登录用户。只返回自己的行，孤儿行（`user_id = 0`）对任何客户都不可见。

| query | 必选 | 说明 |
|---|---|---|
| `p` | 否 | 页码，默认 1 |
| `page_size` | 否 | 每页条数，默认 20，上限 100 |
| `resource_type` | 否 | `asset` \| `asset_group`，缺省返回两类 |
| `status` | 否 | `Active` \| `Processing` \| `Failed` |
| `resource_id` | 否 | 按资产 ID 精确匹配 |
| `start_timestamp` | 否 | 创建时间下界，**秒**级 Unix 时间戳 |
| `end_timestamp` | 否 | 创建时间上界，**秒**级 Unix 时间戳 |

`user_id` 不在此列：归属只取自认证上下文，query 里传了也会被覆盖。
关键字搜索（`keyword`）只在超管视角的 §4.2 生效。

```bash
curl "$SITE/api/volc_asset/self?p=1&page_size=20&resource_type=asset" -b "$COOKIE"
```

```json
{
  "success": true,
  "message": "",
  "data": {
    "page": 1,
    "page_size": 20,
    "total": 3,
    "items": [
      {
        "id": 41,
        "user_id": 7,
        "channel_id": 12,
        "resource_type": "asset",
        "resource_id": "asset-20260410114236-8cdfz",
        "group_id": "assetgroup-20260410113901-qk2mv",
        "name": "主角正脸",
        "asset_type": "Image",
        "status": "Active",
        "created_at": 1775792556,
        "updated_at": 1775792556
      }
    ]
  }
}
```

### 4.2 `GET /api/volc_asset/` — 全部资产

超管。**路径末尾的 `/` 不能省**。

| query | 必选 | 说明 |
|---|---|---|
| `p` / `page_size` | 否 | 同上 |
| `user_id` | 否 | 按归属用户筛；传 `0` 等同不筛（`0` 是「未传」的默认值） |
| `channel_id` | 否 | 按渠道筛 |
| `resource_type` | 否 | `asset` \| `asset_group` |
| `status` | 否 | `Active` \| `Processing` \| `Failed` |
| `resource_id` | 否 | 按资产 ID 精确匹配 |
| `keyword` | 否 | 按 `resource_id` / `name` 模糊匹配 |
| `start_timestamp` | 否 | 创建时间下界，**秒**级 Unix 时间戳 |
| `end_timestamp` | 否 | 创建时间上界，**秒**级 Unix 时间戳 |

不带 `user_id` 时结果中**包含孤儿行**（`user_id = 0`）—— 那正是管理员需要看见并清理的。

```bash
curl "$SITE/api/volc_asset/?p=1&page_size=20&status=Active&keyword=asset-2026" -b "$COOKIE"
```

孤儿行示例（`user_id` 为 0 且无 `username`）：

```json
{
  "id": 88,
  "user_id": 0,
  "channel_id": 12,
  "resource_type": "asset",
  "resource_id": "asset-20260401090000-zzzzz",
  "group_id": "",
  "name": "",
  "asset_type": "Image",
  "status": "Active",
  "created_at": 1775792556,
  "updated_at": 1775792556
}
```

### 4.3 `GET /api/volc_asset/self/quota` — 我的额度

任意已登录用户。只讲「我能建多少、还剩多少」，不暴露渠道名与孤儿数等站点内部账。

无 query 参数。返回：

| 字段 | 类型 | 说明 |
|---|---|---|
| `limit` | int | 该用户的条数上限。`0` 表示未设限，`-1` 表示不限 |
| `used` | int64 | 该用户已占用的条数 |
| `enforced` | bool | 是否真的拦截创建（对应 `volc_asset_setting.enabled`） |
| `count_asset_groups` | bool | 素材组是否计入 `used` |

```json
{
  "success": true,
  "message": "",
  "data": { "limit": 100, "used": 37, "enforced": true, "count_asset_groups": false }
}
```

`enforced` 为 `false` 时 `limit` 只作页面参照，不会拦截创建 —— 这是升级后的默认状态，
免得已经超量的老客户突然建不了素材。

#### 上限从哪来

火山官方**没有查询素材配额的接口**，真实上限只能在控制台 → 配额管理 里看，
按**主账号**计、子账号共用，提额走 配额中心 → 申请配额。所以站点这边的数字
必须手工配：

| 层级 | 配置位置 | 存储 |
|---|---|---|
| 全站默认 | 系统设置 → 运营设置 → 素材库额度 | `volc_asset_setting.default_asset_limit` |
| 单用户提额 | 用户管理 → 行操作 → 素材库额度 | `users.setting` 里的 `volc_asset_limit` |

单用户值优先于全站默认；写 `0` 表示清除覆盖、回落到默认，写 `-1` 表示对该用户不限。

超出上限时，透传链路上的创建动作在**转发前**即被拒，返回 `403`：

```json
{ "error": { "code": "asset_limit_exceeded", "message": "..." } }
```

### 4.4 `GET /api/volc_asset/quota` — 额度概览

超管。对**每个透传渠道**分别回源官方点一次数，与本地账本对账。

无 query 参数。返回 `{"items": [...]}`，每项字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `channel_id` | int | 渠道 id |
| `channel_name` | string | 渠道名 |
| `quota_limit` | int | 额度上限；`0` 表示未设置 |
| `upstream_assets` | int | 官方权威素材数（**含孤儿**） |
| `upstream_groups` | int | 官方权威素材组数 |
| `local_assets` | int64 | 本地账本素材行数 |
| `local_groups` | int64 | 本地账本素材组行数 |
| `orphan_assets` | int64 | 本地已记账但归属为 `0` 的素材数 |
| `orphan_groups` | int64 | 同上，素材组 |
| `unrecorded` | int | 上游有、本地**完全没有**记录的条数 |
| `error` | string | 该渠道回源失败的原因；失败时上游三项为 0，本地项仍有效 |

> `quota_limit` 是**管理员在渠道配置里手填的值**，不是官方返回的。
> 官方没有查询素材资源限额的接口，站点无从得知真实上限，
> 所以这里只做「你自己声明的上限」与「实际用量」的对照。

`orphan` 与 `unrecorded` 是两个不同的坑，处理方式相反：

| | 含义 | 怎么消化 |
|---|---|---|
| `unrecorded` | 上游存在，本地一行都没有 | 跑一次 §4.5 同步，会被认领成孤儿行 |
| `orphan` | 本地有行，但 `user_id = 0` | 管理员在页面上确认后按 §4.6 删除 |

所以正常运维顺序是：先同步把 `unrecorded` 清零，再看 `orphan` 决定删不删。

```bash
curl "$SITE/api/volc_asset/quota" -b "$COOKIE"
```

```json
{
  "success": true,
  "message": "",
  "data": {
    "items": [
      {
        "channel_id": 12,
        "channel_name": "火山透传-北京",
        "quota_limit": 1000,
        "upstream_assets": 214,
        "upstream_groups": 6,
        "local_assets": 210,
        "local_groups": 6,
        "orphan_assets": 3,
        "orphan_groups": 0,
        "unrecorded": 4
      }
    ]
  }
}
```

单渠道 AK/SK 缺失或签名失败时，该项带 `error` 但不影响其他渠道：

```json
{ "channel_id": 13, "channel_name": "火山透传-海外", "quota_limit": 0,
  "upstream_assets": 0, "upstream_groups": 0,
  "local_assets": 87, "local_groups": 2, "orphan_assets": 0, "orphan_groups": 0,
  "unrecorded": 0, "error": "渠道 13 未配置 AK/SK" }
```

### 4.5 `POST /api/volc_asset/sync` — 回源同步

超管。把官方那边的真实清单拉回来，与本地账本对账。

| body / query | 必选 | 说明 |
|---|---|---|
| `channel_id` | 否 | 只同步指定渠道；缺省同步**所有**透传渠道 |

对每条上游记录的三种处置：

| 情况 | 动作 | 计入 |
|---|---|---|
| 上游有、本地无 | 插入一行，`user_id = 0`（孤儿） | `claimed` |
| 上游有、本地有 | 刷新 `name` / `asset_type` / `status` 快照，**归属不动** | `refreshed` |
| 本地有、上游无 | **只计数，不删** | `stale` |

「本地有、上游无」之所以不删：官方列表分页期间客户新建的素材可能落在已翻过的页里，
一次抖动就把客户的资产从账本上抹掉，之后他自己的素材反而校验不过。
`stale` 只是提示管理员去看一眼。

同步是幂等的：重复跑同一渠道，第二次 `claimed` 为 0、`refreshed` 为全量。
遍历上限为 100 页 × 每页 100 条 = 1 万条，超出部分不处理。

```bash
curl -X POST "$SITE/api/volc_asset/sync" -b "$COOKIE" \
  -H "Content-Type: application/json" -d '{"channel_id": 12}'
```

```json
{
  "success": true,
  "message": "",
  "data": {
    "items": [
      { "channel_id": 12, "claimed": 4, "refreshed": 216, "stale": 1 }
    ]
  }
}
```

单渠道失败同样只在该项带 `error`，整体仍返回 `success: true`：

```json
{ "channel_id": 13, "claimed": 0, "refreshed": 0, "stale": 0,
  "error": "签名请求失败: 403 InvalidAccessKey" }
```

### 4.6 `DELETE /api/volc_asset/:id` — 删除资产

超管。`:id` 是**账本行 id**（§三表格里的 `id`），不是火山的 `resource_id`。

执行顺序是**先删上游、成功后再删本地行**：

1. 用渠道 AK/SK 调官方 `DeleteAsset` / `DeleteAssetGroup`；
2. 官方返回成功后，删除 `volc_assets` 里那一行。

顺序不能反。先删本地行的话，一旦上游删除失败，这条素材就变成
「上游还占额度、本地已无记录」—— 下次同步又会把它认领成孤儿，
额度永远降不下来，而且没人知道它本来属于谁。

反过来（当前顺序）最坏情况只是上游已删、本地行还在，
下次同步会把它计入 `stale`，管理员再点一次删除即可。

| 返回 | 含义 |
|---|---|
| `success: true` | 上游与本地都已删除 |
| `success: false` + `无效的资产记录 id` | `:id` 不是正整数 |
| `success: false` + `资产所属渠道 #N 不可用` | 渠道被删或被禁用，拿不到 AK/SK |
| `success: false` + `官方删除失败,已保留本地记录: …` | 上游删除失败，**本地行保留**，可直接重试 |

```bash
curl -X DELETE "$SITE/api/volc_asset/88" -b "$COOKIE"
```

```json
{ "success": true, "message": "", "data": null }
```

> **删素材组是级联的，且不可逆。** 官方 `DeleteAssetGroup` 会连带删光组内全部素材，
> 所以删除一行 `resource_type = asset_group` 之后，站点也会把该渠道下
> `group_id` 等于这个组的本地行一并清掉。
>
> 不跟着级联的话，那些行会变成永远删不掉的记录 —— 再点删除时官方报「不存在」，
> 页面上却还列着它们。

## 五、页面入口

两套主题都有，路径不同，功能一致：

| 主题 | 路径 |
|---|---|
| default | `/volc-assets` |
| classic | `/console/volc-assets` |

页面对**任何已登录用户**开放，普通用户与管理员只有「我的资产」一张表，
额度横在表格上方；超管多出「全部资产」页签和右上角的「回源同步」按钮，
额度换成按渠道对账的那一版。

classic 主题的侧边栏模块开关键为 `volcAssets`（位于 `console` 分组），
在 系统设置 → 运营设置 → 侧边栏模块 里可关闭。

## 六、相关实现

| 文件 | 职责 |
|---|---|
| `router/api-router.go` | 五条路由与中间件 |
| `controller/volc_asset.go` | 五个 handler、`volcAssetDto` 与用户名填充 |
| `model/volc_asset.go` | `volc_assets` 表、归属校验、upsert |
| `service/volc_asset_sync.go` | 同步与额度对账 |
| `service/volc_asset_admin.go` | 控制面签名客户端（回源列表 / 删除） |
| `service/volc_asset_isolation.go` | 透传链路上的归属校验与记账 |
| `web/default/src/features/volc-assets/` | default 主题页面 |
| `web/classic/src/pages/VolcAssets/` | classic 主题页面 |

透传链路本身（客户怎么创建素材、怎么在视频提交里引用 `asset://`）见
[客户接口文档](./cn-client-api.md)。
