# 字节火山透传渠道设计

日期：2026-09-13
状态：待实施

## 1. 目标

新增一个「字节火山透传」渠道类型，让客户以**与官方完全一致**的方式调用火山方舟（国内）与 BytePlus ModelArk（海外）：

- 请求路径、query、请求体、响应体、错误码 **全部与官方一致**
- 客户侧只有两处不同：URL 前缀换成本站点、密钥换成本站点令牌
- 站点仅托管 AK/SK/密钥等凭据，不改写业务语义
- 视频生成接口按既有 Seedance 计价矩阵计费，其余接口透传不计费

## 2. 事实基础

官方文档已全量落地（`接口文档/seedacne透传文档/`，457 份 md）：

| 侧 | 数据面 host | 控制面 host | 签名 region |
|---|---|---|---|
| 国内 | `ark.cn-beijing.volces.com` | `ark.cn-beijing.volcengineapi.com` | `cn-beijing` |
| 海外 | `ark.ap-southeast.bytepluses.com` | `ark.ap-southeast-1.byteplusapi.com` | `ap-southeast-1` |

接口规模：国内 84 条数据面路径 + 112 个控制面 Action；海外数据面 11 条 + 控制面 12 个 Action。

**关键事实（已核对文档，推翻了早期假设）**：

1. 国内控制面是 `ark.cn-beijing.volcengineapi.com`，**不是** `open.volcengineapi.com`
   （后者在 389 份国内文档中零出现）。现有 `byteplus_asset.go:baseEndpoint()` 的
   `cn-* → open.volcengineapi.com` 推导是素材库单接口的特例，不可推广到 112 个 Action。
2. 海外数据面 host 用 `ap-southeast`（无 `-1`），控制面 host 与签名 CredentialScope
   用 `ap-southeast-1`（有 `-1`）。两者**不能互推**，必须分开存储。
3. 数据面认证为 `Authorization: Bearer <API Key>`；控制面为 AK/SK HMAC-SHA256 签名
   （`Service=ark`），两套认证并存。
4. Files 上传支持二进制 multipart，body **不能假设为 JSON**。

## 3. 架构

### 3.1 路径映射

```
客户 → POST https://<站点>/volc/api/v3/contents/generations/tasks
       Authorization: Bearer sk-<站点令牌>
       {官方原样 body}

站点 → POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks
       Authorization: Bearer <渠道密钥>
       {同一份 body，字节不变}
```

前缀 `/volc` 剥离后，**剩余 path + query 原样拼到渠道 BaseURL 之后**。
这是通配转发，不枚举接口 —— 官方新增接口零改造自动可用，这是覆盖
84+112 条接口面的唯一可行方式。

海外使用同一套路由，靠渠道 BaseURL 区分（填 `https://ark.ap-southeast.bytepluses.com`）。

### 3.2 双认证判定

| 请求形态 | 认证 | 上游 host |
|---|---|---|
| path 非空（`/api/v3/...`） | `Authorization: Bearer <渠道密钥>` | 渠道 BaseURL |
| path 为 `/` 且带 `?Action=` | `common/volcsign` AK/SK 签名，Service=`ark` | 控制面 host（独立解析） |

控制面 host 解析优先级：渠道显式覆盖字段 > 按签名 region 推导
（`cn-*` → `ark.{region}.volcengineapi.com`，其余 → `ark.{region}.byteplusapi.com`）。

### 3.3 字节流透传

请求与响应 body 均按 `io.Copy` 字节流转发：

- 保留原始 `Content-Type`（含 multipart boundary）
- 不做 JSON 反序列化、不重新 marshal
- 响应保留上游 status code 与关键 header，SSE 流式原样穿透
- 仅当 `Content-Type: application/json` 且命中生成类路径时，peek 一份 body
  供计费读取 `resolution`/`duration`/`content[].type`，peek 后回填流

### 3.4 计费

复用既有 `relay/channel/task/seedance` 计价包，不新增计费逻辑：

```go
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
    if seedance.IsSeedance2(info.OriginModelName) {
        return seedance.EstimateBilling(c, info)
    }
    return nil
}
```

已验证 `seedance/billing.go` 的 `DetectResolution` / `HasVideoInput` 均有
raw body 回退路径，且 `relay_info.go:742` 的 `TaskSubmitReq.UnmarshalJSON`
已支持顶层 `duration` 的 int/string 双形态 —— **官方原生请求体无需转换即可完成计费**。
分辨率档位、限时折扣（`billing_setting.ResolveVideoPromo`）自动生效。

| 接口 | 计费 |
|---|---|
| `POST /api/v3/contents/generations/tasks` | Seedance 矩阵预扣 + 完成结算 |
| `POST /api/v3/images/generations` | 图片模型计价 |
| 任务查询/删除、Files、112 个控制面 Action | 不计费 |

### 3.5 任务查询回源

现有 `RelayTaskFetch`（`relay/relay_task.go:289`）读本地 `model.Task` 快照并包一层
`{"code":"success","data":{...}}`，与「响应与官方一致」冲突。

透传渠道**不走** `RelayTaskFetch`，改为：

1. `GET /volc/api/v3/contents/generations/tasks/{id}` 直接回源官方
2. 原样回写官方裸响应（无 `code`/`data` 外壳），`video_url` 永远是当前有效值
3. 旁路解析响应中的 `status`，更新本地 task 表用于计费结算，不参与响应构造

### 3.6 渠道定位与同源约束

- 提交：靠请求体 `model` 字段走既有 `Distribute()` 选渠道
- 查询/删除：无 `model` 字段，靠 `model.GetByOnlyTaskId(taskId)` 反查提交时记录的
  `ChannelId`（`Task.TaskID` 已有索引），**不重新选渠道**

同源约束（来自 `doubao/adaptor.go` 既有教训）：提交与查询必须落到同一渠道，
否则任务卡 `in_progress` 且额度已预扣。

## 4. 配置字段

复用 `dto.ChannelOtherSettings` 既有字段，新增两个：

| 字段 | 用途 | 默认 |
|---|---|---|
| `BytePlusAccessKey` | 控制面签名 AK（已存在） | — |
| `BytePlusSecretKey` | 控制面签名 SK（已存在） | — |
| `BytePlusProjectName` | 资源项目名（已存在） | `default` |
| `VolcSignRegion` | 签名 region（新增，与数据面 host 解耦） | 国内 `cn-beijing` / 海外 `ap-southeast-1` |
| `VolcOpenAPIEndpoint` | 控制面 host 覆盖（新增，私有化网关用） | 空 = 按 region 推导 |
| 渠道 Key | 数据面 Bearer 密钥 | — |
| 渠道 BaseURL | 数据面 host | — |

## 5. 文件清单

### 新增

| 文件 | 作用 |
|---|---|
| `relay/channel/volcpassthrough/adaptor.go` | 透传适配器主体 |
| `relay/channel/volcpassthrough/endpoint.go` | 数据面/控制面 host 与签名 region 解析 |
| `relay/channel/volcpassthrough/constants.go` | ChannelName、ModelList |
| `controller/relay_passthrough.go` | 通用透传 handler（字节流转发） |
| `router/volc-passthrough-router.go` | `/volc/*` 路由组 |
| `relay/channel/volcpassthrough/endpoint_test.go` | host 推导边界测试 |
| `relay/channel/volcpassthrough/adaptor_test.go` | URL 拼接、认证判定、model 替换测试 |

### 修改

| 文件 | 改动 |
|---|---|
| `constant/channel.go` | 新增 `ChannelTypeVolcPassthrough`、名称、默认 BaseURL |
| `router/main.go` | 注册透传路由组 |
| `dto/channel_settings.go` | 新增 `VolcSignRegion`、`VolcOpenAPIEndpoint` 及解析方法 |
| `middleware/distributor.go` | `getModelRequest` 新增透传路径分支 |
| `web/default/src/features/channels/*` | 渠道表单字段与类型 |

### 不改动

`relay/channel/task/doubao/*`、`relay/channel/task/seedance/*`、`common/volcsign`
—— 全部原样复用，零回归风险。

## 6. 风险

| 风险 | 缓解 |
|---|---|
| 通配转发可能暴露非预期上游接口 | 路径前缀白名单限定 `/api/v3/`、`/api/compatible/` 与控制面 `/?Action=` |
| 客户令牌被误透传到上游 | 复用 `api_request.go` 既有凭据跳过名单，认证头强制覆盖而非追加 |
| 上游 4xx/5xx 被误判为渠道故障触发禁用 | 透传错误不计入渠道健康度，原样回传 |
| 计费 peek 破坏 multipart 请求 | peek 仅在 `Content-Type: application/json` 时进行 |
