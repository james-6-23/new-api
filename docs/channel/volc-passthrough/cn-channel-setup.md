# 国内（火山方舟）· 渠道配置使用文档

面向站点管理员。配置一个「字节火山透传」渠道，把火山方舟的
**Seedance 视频生成与私域素材库**透出给客户。

## 一、前置：在火山方舟侧准备凭据

| 凭据 | 用途 | 获取位置 |
|---|---|---|
| **API Key** | 数据面 `/api/v3/...` 的 `Bearer` 认证 | 火山方舟控制台 → API Key 管理 |
| **AccessKey (AK)** | 控制面 `?Action=` 的签名 | 火山引擎控制台 → 访问控制 → 访问密钥 |
| **SecretKey (SK)** | 同上 | 同上（只在创建时可见，务必留存） |
| **项目名** | 资源归属项目 | 火山引擎「资源项目」，默认 `default` |

> 数据面密钥与控制面 AK/SK 是**两套独立凭据**，不能互相替代。
> 只用数据面接口（对话、视频、图片、Files）时，AK/SK 可以不填。

## 二、在 new-api 新建渠道

**渠道 → 添加渠道**，按下表填写：

| 字段 | 填写值 | 说明 |
|---|---|---|
| 类型 | `字节火山透传` | 渠道类型 60 |
| 名称 | 自定 | 例如 `火山方舟-北京-生产` |
| 代理地址（BaseURL） | `https://ark.cn-beijing.volces.com` | **数据面** host；留空用此默认值 |
| 密钥 | 火山方舟 API Key | 数据面 Bearer 凭据 |
| 分组 | 按站点习惯 | 客户令牌的分组要能命中此渠道 |
| 模型 | 需要计费的视频模型名 | 见下方「模型列表怎么填」 |

展开 **控制面签名（Control plane signing）** 区块：

| 字段 | 填写值 | 留空行为 |
|---|---|---|
| Access Key (AK) | 火山引擎 AK | 控制面接口返回签名失败 |
| Secret Key (SK) | 火山引擎 SK | 同上 |
| Project Name | `default` 或自定 | 默认 `default` |
| Signing Region | `cn-beijing` | BaseURL 含 `volces.com`/`volcengine` → 自动取 `cn-beijing` |
| Control Plane Endpoint | `https://ark.cn-beijing.volcengineapi.com` | 按 Signing Region 推导为 `https://ark.<region>.volcengineapi.com` |
| 素材库额度上限 | 你在火山控制台看到的资产条数上限 | `0` = 未设置，额度页只显示已用量不做对照 |

> **两个 host 不是一个。** 数据面 `ark.cn-beijing.volces.com`，
> 控制面 `ark.cn-beijing.volcengineapi.com`。BaseURL 只填数据面，控制面由站点单独解析。
> 私有化网关才需要手工覆盖 Control Plane Endpoint。

#### Project Name 与素材隔离的关系

`Project Name` 是火山引擎的**资源项目**，属 IAM 预建概念，只能在火山控制台创建，
站点无法按令牌动态开。所以一个渠道下所有客户的素材都落在同一个项目里，
**客户之间的隔离由站点记账完成**，不依赖这个字段。

改这个值只影响新建素材落在哪个火山项目下，不会迁移已有素材，
也不会改变站点账本里的归属关系。多渠道想在火山侧分开看，才需要分别填不同项目名。

#### 素材库额度上限为什么要手填

官方没有查询素材限额的 API —— 全量 Action 里资产相关只有 12 个 CRUD，
`GetAFPUsage` / `GetInferenceUsage` / `ListModelRateLimit` 都是模型调用量与限流，
素材总量上限官方只在控制台看板给。

所以这里填的是**你自己声明的上限**，站点用它跟 `ListAssets` 回源来的真实已用量做对照，
在 [素材库管理接口](./asset-admin-api.md) 的额度页显示。填错不影响任何接口行为，
只影响那张对照表好不好看。

控制台里这个数在 配额管理 里看，按**主账号**计、子账号共用，提额走 配额中心 → 申请配额。

> 别把它跟**每账号素材条数上限**混了。渠道上这一项是给超管对账用的参照，不拦请求；
> 真正会拦住客户创建素材的是 系统设置 → 运营设置 → 素材库额度 里的
> `volc_asset_setting`（全站默认 + 用户管理里的单用户提额），超量时返回 `403`
> `asset_limit_exceeded`。前者是「这个渠道在火山那边还能放多少」，
> 后者是「每个客户在本站能占多少」。

### 模型列表怎么填

只影响**计费的视频生成接口**。模型名照抄官方，客户提交时 body 里的 `model` 必须在此列表内：

```
doubao-seedance-2-5-260628
doubao-seedance-2-0-260128
doubao-seedance-2-0-fast-260128
doubao-seedance-2-0-mini-260615
```

素材库那 12 个控制面 Action 与任务查询/删除不看模型列表，只要令牌能命中本渠道
所在分组就能调。

### 其他可选项

| 项 | 位置 | 说明 |
|---|---|---|
| 代理 | 渠道设置 `proxy` | 走 socks5/http 代理访问上游 |
| 模型重定向 | 渠道 `model_mapping` | 提交视频任务时会把 body 里的 `model` 改写为上游名 |

## 三、计费配置

**只有 `POST /api/v3/contents/generations/tasks` 计费**，其余接口零扣费。

计价链路：

1. 提交时按模型 + 分辨率 + 是否含视频输入预扣额度；
   Seedance 2.x 走官方二维矩阵倍率（`relay/channel/task/seedance/pricing.go`），其余模型走基础模型倍率。
2. 任务完成后按官方返回的 `usage.total_tokens` 重算，多退少补。

因此需要在 **运营设置 → 模型倍率** 里为上述视频模型配好 `model_ratio`。
限时折扣走 `billing_setting.video_promo`，与本渠道无关，配置方式同既有视频渠道。

## 四、验证渠道是否可用

> 客户调用时**数据面路径与官方逐字一致**，只把官方域名换成站点域名；
> 控制面因官方挂在域名根路径、站点根被前端占用，保留 `/volc` 前缀。

建好渠道后，用一个能命中该分组的站点令牌自测：

```bash
SITE=https://your-site.com
TOKEN=sk-your-site-token

# 1) 数据面：列文件（不计费）
curl "$SITE/api/v3/files" -H "Authorization: Bearer $TOKEN"

# 2) 控制面：列素材资产（不计费，验证 AK/SK 是否正确）
curl -X POST "$SITE/volc/?Action=ListAssets&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"PageNumber":1,"PageSize":10}'

# 3) 数据面：提交视频任务（计费）
curl -X POST "$SITE/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"doubao-seedance-2-5-260628","content":[{"type":"text","text":"一只猫在沙滩上奔跑 --rs 1080p --dur 5"}]}'
```

## 五、常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `分组 X 下没有可用的字节火山透传渠道` | 令牌分组与渠道分组不匹配，或渠道被禁用 | 核对渠道分组、状态 |
| 控制面返回签名错误 | AK/SK 未填或 Signing Region 与 host 不一致 | 国内必须是 `cn-beijing` |
| 数据面 401 | 渠道「密钥」不是方舟 API Key（误填了 AK） | 换成 API Key |
| 视频任务长期 `in_progress` | 提交与查询落到不同渠道 | 站点已按 task id 反查提交渠道；若任务非本站提交则属预期 |
| 模型名不在列表被拒 | 渠道模型列表缺该模型 | 补进渠道模型列表 |

## 六、支持的接口范围

本渠道**只放行 16 个入口**，白名单是精确匹配，不是前缀通配：

| 面 | 放行内容 | 计费 |
|---|---|---|
| 数据面 | `POST /api/v3/contents/generations/tasks` | **计费** |
| 数据面 | `GET /api/v3/contents/generations/tasks`（列表） | 否 |
| 数据面 | `GET /api/v3/contents/generations/tasks/{id}` | 否 |
| 数据面 | `DELETE /api/v3/contents/generations/tasks/{id}` | 否 |
| 控制面 | 素材组 5 个 Action：`CreateAssetGroup` / `GetAssetGroup` / `ListAssetGroups` / `UpdateAssetGroup` / `DeleteAssetGroup` | 否 |
| 控制面 | 素材 5 个 Action：`CreateAsset` / `GetAsset` / `ListAssets` / `UpdateAsset` / `DeleteAsset` | 否 |
| 控制面 | 真人认证 2 个 Action：`CreateVisualValidateSession` / `GetVisualValidateResult` | 否 |

其余一律 `404 endpoint_not_supported`，包括 `/api/v3/chat/completions`、
`/api/v3/embeddings`、`/api/compatible/`、`/api/coding/`，以及另外约 100 个控制面
Action（`Endpoint*`、`Model*`、`Usage*`、`RateLimit*` 等）。

**这是故意的。** 早期版本放行整个 `/api/v3/` 前缀，等于把 chat/completions、embedding
等全部官方能力免费开给持站点令牌的客户 —— 那些路径不走计费链路，额度扣不到。
控制面同理：`Endpoint*` / `Model*` 那些 Action 能改动站点自己的火山账号配置。

因此**官方新增接口不会自动可用**，需要改 `relay/channel/volcpassthrough/endpoint.go`
里的白名单并重新编译。

完整 curl 清单见 [国内 · 客户端调用文档](./cn-client-api.md)。
站点侧的素材归属账本与管理接口见 [素材库管理接口](./asset-admin-api.md)。
