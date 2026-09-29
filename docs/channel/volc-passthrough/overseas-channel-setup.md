# 海外（BytePlus ModelArk）· 渠道配置使用文档

面向站点管理员。配置一个「字节火山透传」渠道指向 BytePlus ModelArk 海外站点。

## 一、⚠️ 先记住 host 陷阱

海外数据面与控制面的 host **差一个 `-1`，且不可互推**：

| 面 | Host | 域名 | region 后缀 |
|---|---|---|---|
| 数据面 `/api/v3/...` | `ark.ap-southeast.bytepluses.com` | `bytepluses.com`（带 s） | **无** `-1` |
| 控制面 `?Action=` | `ark.ap-southeast-1.byteplusapi.com` | `byteplusapi.com` | **有** `-1` |
| 签名 CredentialScope | — | — | `ap-southeast-1`（**有** `-1`） |

填反了会得到 DNS 解析失败或签名错误。站点把这两处**分开解析**，正是为了处理这个差异。

其他区域数据面 host：

| 区域 | 数据面 BaseURL |
|---|---|
| ap-southeast-1（新山 Johor） | `https://ark.ap-southeast.bytepluses.com` |
| eu-west-1 | `https://ark.eu-west.bytepluses.com` |

## 二、前置：在 BytePlus 侧准备凭据

| 凭据 | 用途 | 获取位置 |
|---|---|---|
| **API Key** | 数据面 `Bearer` 认证 | BytePlus ModelArk Console → API Keys |
| **AccessKey (AK)** | 控制面签名 | BytePlus Console → Access Control → Access Keys |
| **SecretKey (SK)** | 同上 | 同上（仅创建时可见） |
| **Project Name** | 资源项目，默认 `default` | BytePlus 资源项目 |

## 三、在 new-api 新建渠道

| 字段 | 填写值 |
|---|---|
| 类型 | `字节火山透传`（60） |
| 名称 | 例如 `BytePlus-ap-southeast-生产` |
| 代理地址（BaseURL） | `https://ark.ap-southeast.bytepluses.com` ← **无 `-1`** |
| 密钥 | BytePlus ModelArk API Key |
| 分组 | 按站点习惯 |
| 模型 | 需计费的视频模型名（见下） |

**控制面签名（Control plane signing）** 区块：

| 字段 | 填写值 | 留空行为 |
|---|---|---|
| Access Key (AK) | BytePlus AK | 控制面签名失败 |
| Secret Key (SK) | BytePlus SK | 同上 |
| Project Name | `default` | 默认 `default` |
| Signing Region | `ap-southeast-1` ← **有 `-1`** | BaseURL 不含 `volces.com` → 自动取 `ap-southeast-1` |
| Control Plane Endpoint | `https://ark.ap-southeast-1.byteplusapi.com` | 按 region 推导为 `https://ark.<region>.byteplusapi.com` |
| 素材库额度上限 | 你在 BytePlus 控制台看到的资产条数上限 | `0` = 未设置，额度页只显示已用量 |

`Project Name` 是 BytePlus 的资源项目（IAM 预建，站点无法按令牌动态开），
所以一个渠道下所有客户的素材都落在同一项目里，**客户之间的隔离由站点记账完成**。
额度上限官方没有查询 API，只能手填，站点拿它跟 `ListAssets` 回源的真实用量做对照。
它不拦请求；拦住客户创建素材的是 系统设置 → 运营设置 → 素材库额度 里的每账号条数上限。
两者的详细说明见 [国内 · 渠道配置](./cn-channel-setup.md) 的同名小节。

默认推导对 `ap-southeast` 已经正确，通常无需手工填写这两项。
**eu-west 等其他区域必须显式填写 Signing Region**，否则会按默认 `ap-southeast-1` 签名。

### 模型列表

```
dreamina-seedance-2-5-260628
dreamina-seedance-2-0-260128
dreamina-seedance-2-0-fast-260128
dreamina-seedance-2-0-mini-260615
```

海外用 `dreamina-*` 命名，国内用 `doubao-*`。计价矩阵两边都已内置，
相对倍率一致、绝对单价随市场不同（海外 USD/M，国内 元/M）。

## 四、计费

与国内一致：**只对 `POST /api/v3/contents/generations/tasks` 计费**。
提交时按模型 + 分辨率 + 是否含视频输入预扣，完成后按 `usage.total_tokens` 重算。

## 五、验证

```bash
SITE=https://your-site.com
TOKEN=sk-your-site-token

# 数据面
curl "$SITE/api/v3/files" -H "Authorization: Bearer $TOKEN"

# 控制面（验证 AK/SK + ap-southeast-1 签名）
curl -X POST "$SITE/volc/?Action=ListAssets&Version=2024-01-01" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"PageNumber":1,"PageSize":10}'

# 视频提交（计费）
curl -X POST "$SITE/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"dreamina-seedance-2-5-260628","content":[{"type":"text","text":"a cat running on the beach --rs 1080p --dur 5"}]}'
```

## 六、常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| 数据面 DNS 解析失败 | BaseURL 误填成 `ark.ap-southeast-1.bytepluses.com` | 去掉 `-1` |
| 控制面签名错误 | Signing Region 填成 `ap-southeast`（少 `-1`） | 改为 `ap-southeast-1` |
| 控制面 404 | Control Plane Endpoint 误填数据面 host | 改为 `*.byteplusapi.com` |
| eu-west 区域签名失败 | 未显式填 Signing Region，落到默认 `ap-southeast-1` | 显式填 `eu-west-1` |
| `CasePlatformV1*` 等 Action 返回 404 `endpoint_not_supported` | 不在白名单内 | 属预期，见 §七 |

## 七、支持的接口范围

与国内渠道完全一致：**只放行 16 个入口**，白名单精确匹配。

| 面 | 放行内容 | 计费 |
|---|---|---|
| 数据面 | `POST /api/v3/contents/generations/tasks` | **计费** |
| 数据面 | `GET /api/v3/contents/generations/tasks`（列表） | 否 |
| 数据面 | `GET /api/v3/contents/generations/tasks/{id}` | 否 |
| 数据面 | `DELETE /api/v3/contents/generations/tasks/{id}` | 否 |
| 控制面 | 素材组 5 个 + 素材 5 个 + 真人认证 2 个 Action，共 12 个 | 否 |

其余一律 `404 endpoint_not_supported`。这不是覆盖度问题，是**故意收窄**：
放行整个 `/api/v3/` 前缀等于把 chat/completions、embedding 免费开给持站点令牌的客户
（那些路径不走计费链路），而 `Endpoint*` / `Model*` 那类控制面 Action 能改动站点自己的
BytePlus 账号配置。

因此**官方新增接口不会自动可用**，需要改
`relay/channel/volcpassthrough/endpoint.go` 里的白名单并重新编译。

海外与国内的白名单是同一份，区别只在 host、签名 region 与模型命名（`dreamina-*`）。
完整 curl 清单见 [海外 · 客户端调用文档](./overseas-client-api.md)，
站点侧的素材归属账本与管理接口见 [素材库管理接口](./asset-admin-api.md)。
