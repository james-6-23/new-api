# 任务提交/查询错误透传 + 脱敏 + OpenAI 错误格式 (Task Upstream Error Passthrough)

- 日期: 2026-07-31
- 状态: 已确认设计，随本次实现落地
- 范围: 异步任务(seedance 系列为主诉求)在 OpenAI video 端点(`/v1/videos*`)的错误返回链路;通用于所有 task 渠道(sora / seedance3rd / doubao / vidu / …)。
- 相关规则: CLAUDE.md Rule 1 (JSON wrapper)、Rule 5 (受保护标识)

## 1. 背景(核实的现状问题)

seedance 模型经 `POST /v1/videos`(sora 形状端点)提交,上游报错时客户拿到的是**错误的错误**:

1. `relay/relay_task.go` 步骤 9:上游非 200 → `TaskErrorWrapper(fmt.Errorf("%s", body), "fail_to_fetch_task", status)`,客户收到 `{"code":"fail_to_fetch_task","message":"{\"error\":{…原始上游 JSON…}}","data":null}` —— message 是双重编码 JSON 串,上游真实的 message/code 没被提取。
2. 脱敏缺失:`TaskErrorWrapper` 仅当文本含 post/dial/http 时才走 `MaskSensitiveInfo`;上游 body 里的 URL/IP(如"failed to download https://内部素材地址")多数场景原样透出。上游 401/403(渠道 key 问题)也原样透传,客户会误以为自己的 key 错了,还可能泄露渠道侧信息。
3. 格式不对:`/v1/videos*` 是 OpenAI 形状端点,报错应是 OpenAI 错误格式 `{"error":{"message","type","code"}}`;现在返回内部 TaskError 形状。
4. 异步失败(轮询)侧:`task.FailReason = taskResult.Reason` 未脱敏;sora `ConvertToOpenAIVideo` 原样返回 task.Data(含上游 error.message);seedance3rd 把 `tk.Error.Message` 原样映射。

## 2. 设计决策

| 主题 | 决策 |
|---|---|
| 提取上游错误 | 新增 `service.TaskErrorFromUpstreamBody(statusCode, body)`:复用 `dto.GeneralErrorResponse`(TryToOpenAIError 优先,ToMessage 兜底)提取真实 message + code;解析失败时截断(512B)后作 message。 |
| 脱敏 | 所有透传 message 一律过 `common.MaskSensitiveInfo`(mask URL/IP/域名,现成函数)。 |
| 上游 401/403 | **不透传**(渠道凭证问题,与客户无关):message 固定「上游渠道认证失败，请联系管理员」,状态码改 503(避免客户误判自己的 key;5xx 也让重试逻辑换渠道重试)。 |
| 状态码 | 其余保留上游原状态码(400/404/422/429/5xx…),`shouldRetryTaskRelay` 行为不变(400 不重试,429/5xx 重试)。 |
| OpenAI 错误格式 | `respondTaskError` 检测 `c.Request.URL.Path` 前缀 `/v1/videos` → 输出 `{"error":{"message","type","code"}}`(type 按状态码映射:429 rate_limit_error / 4xx invalid_request_error / 5xx upstream_error);其他任务端点(`/v1/video/generations`、suno 等)保持原 TaskError 形状(向后兼容)。429 的中文提示改写保留。 |
| 异步失败脱敏 | `task_polling.go` 写 `task.FailReason` 时脱敏(单点覆盖所有渠道的任务列表/fetch 展示);sora `ConvertToOpenAIVideo` 对 `data.error.message` 用 sjson 脱敏;seedance3rd 对 `ov.Error.Message` 脱敏。 |
| 不改 | 各 adaptor 的 DoResponse 解析失败分支(本就是网关内部错误);同步文本 relay 的错误链路(已有 RelayErrorHandler);任务成功路径。 |

## 3. 改动点

- `service/error.go`:新增 `TaskErrorFromUpstreamBody`。
- `relay/relay_task.go` 步骤 9 非 200 分支改调新函数。
- `controller/relay.go` `respondTaskError`:`/v1/videos*` 输出 OpenAI 错误格式(新增 `taskErrorToOpenAIFormat` 辅助 + 状态码→type 映射)。
- `service/task_polling.go`:`task.FailReason = common.MaskSensitiveInfo(taskResult.Reason)`。
- `relay/channel/task/sora/adaptor.go` `ConvertToOpenAIVideo`:data 中 `error.message` 存在时 sjson 脱敏。
- `relay/channel/task/seedance3rd/adaptor.go` `ConvertToOpenAIVideo`:`ov.Error.Message` 脱敏。

## 4. 验证

- 单测:`TaskErrorFromUpstreamBody`(OpenAI error JSON / message-only / 非 JSON 截断 / URL 脱敏 / 401→503 改写 / code 提取);`respondTaskError` 两种形状;sora/seedance3rd fetch 脱敏。
- `go build ./...`;`go test ./service/... ./relay/... ./controller/`(限相关包);`go vet`。

## 5. 范围外

- 同步 relay(chat/completions)错误链路;midjourney/suno 专用错误形状;task.Data 全量脱敏(仅 error.message)。
