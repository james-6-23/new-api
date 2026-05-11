# Seedance 2.0 视频生成接口文档

> 来源：https://tikapi.apifox.cn/455662379e0
>
> 本文档为 Seedance 2.0 视频模型的 **官方格式参数** 接口规范（`/v1/videos`）。

---

## 接口概览

| 项目 | 说明 |
|------|------|
| **请求方式** | `POST` |
| **请求路径** | `/v1/videos` |
| **鉴权方式** | Header `Authorization`，值为 API Key（如 `sk-xxx`） |
| **Content-Type** | `application/json` |

---

## 请求参数

### 顶层参数

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | 调用的模型 ID，如 `doubao-seedance-2-0-260128` |
| `prompt` | string | 是 | 文本提示词，描述期望生成的视频内容 |
| `metadata` | object | 是 | 媒体信息容器，包含输入素材和生成参数 |

### metadata 对象

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | array | 是 | 输入素材数组，支持图片、视频、音频 |
| `generate_audio` | boolean | 是 | 是否生成有声视频。`true` = 有声，`false` = 无声 |
| `ratio` | string | 是 | 视频宽高比 |
| `duration` | integer | 是 | 视频时长（秒） |
| `watermark` | boolean | 是 | 是否添加水印 |

### metadata.content 数组项

每个数组项代表一个输入素材：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | string | 是 | 素材类型，枚举值见下表 |
| `role` | string | 是 | 素材角色/用途，枚举值见下表 |
| `image_url` | object | 条件必填 | 当 `type = "image_url"` 时使用 |
| `video_url` | object | 条件必填 | 当 `type = "video_url"` 时使用 |
| `audio_url` | object | 条件必填 | 当 `type = "audio_url"` 时使用 |

#### type 枚举值

| 值 | 说明 |
|----|------|
| `image_url` | 图片素材 |
| `video_url` | 视频素材 |
| `audio_url` | 音频素材 |

#### role 枚举值

| 值 | 说明 |
|----|------|
| `first_frame` | 首帧图片 |
| `last_frame` | 尾帧图片 |
| `reference_image` | 参考图像 |
| `reference_video` | 参考视频 |
| `reference_audio` | 参考音频 |

#### URL 对象结构

- `image_url.url`（string）：图片地址，支持公网 URL 或 Base64 编码
- `video_url.url`（string）：视频地址，仅支持公网 URL
- `audio_url.url`（string）：音频地址，支持公网 URL 或 Base64 编码

---

## 模型能力

### seedance 2.0 & 2.0 fast

- **多模态参考生视频**：输入参考图片（0~9）+ 参考视频（0~3）+ 参考音频（0~3）+ 文本提示词（可选）→ 生成 1 个目标视频。不可单独输入音频，应至少包含 1 个参考视频或图片。支持生成全新视频、编辑视频、延长视频。
- **图生视频-首尾帧**：输入首帧图片 + 尾帧图片 + 文本提示词（可选）→ 生成 1 个目标视频。
- **图生视频-首帧**：输入首帧图片 + 文本提示词（可选）→ 生成 1 个目标视频。
- **文生视频**：输入文本提示词 → 生成 1 个目标视频。

> **注意**：seedance 2.0 系列模型不支持直接上传含有真人人脸的参考图/视频。

---

## 参数约束

### 提示词

- **语言支持**：中文、英文、日语、印尼语、西班牙语、葡萄牙语
- **字数建议**：中文不超过 500 字，英文不超过 1000 词

### 宽高比（ratio）枚举

| 值 | 说明 |
|----|------|
| `16:9` | 横屏 |
| `4:3` | 横屏 |
| `1:1` | 正方形 |
| `3:4` | 竖屏 |
| `9:16` | 竖屏 |
| `21:9` | 超宽屏 |
| `adaptive` | 自适应 |

### 视频时长（duration）

| 模型 | 取值范围 |
|------|----------|
| seedance 2.0 & 2.0 fast | [4, 15] 秒，或 `-1`（模型自主选择） |

### 各分辨率下的宽高像素值（seedance 2.0 系列）

| 分辨率 | 16:9 | 4:3 | 1:1 | 3:4 | 9:16 | 21:9 |
|--------|------|-----|-----|-----|------|------|
| 480p | 864x496 | 752x560 | 640x640 | 560x752 | 496x864 | 992x432 |
| 720p | 1280x720 | 1112x834 | 960x960 | 834x1112 | 720x1280 | 1470x630 |
| 1080p | 1920x1080 | 1664x1248 | 1440x1440 | 1248x1664 | 1080x1920 | 2206x946 |

### 图片要求

- **格式**：jpeg、png、webp、bmp、tiff、gif
- **宽高比**（宽/高）：(0.4, 2.5)
- **宽高长度**（px）：(300, 6000)
- **大小**：单张 < 30 MB，请求体 < 64 MB

### 视频要求

- **格式**：mp4、mov
- **分辨率**：480p、720p、1080p
- **时长**：单个 [2, 15]s，最多 3 个，总时长 ≤ 15s
- **宽高比**（宽/高）：[0.4, 2.5]
- **宽高像素**：[300, 6000]
- **总像素数**：[409600, 2086876]
- **大小**：单个 ≤ 50 MB
- **帧率**：[24, 60] FPS

### 音频要求

- **格式**：wav、mp3
- **时长**：单个 [2, 15]s，最多 3 段，总时长 ≤ 15s
- **大小**：单个 ≤ 15 MB，请求体 ≤ 64 MB
- 不可单独输入音频，应至少包含 1 个参考视频或图片

---

## 其他可选参数（火山引擎原生接口）

以下参数在火山引擎原生接口（`/api/v3/contents/generations/tasks`）中支持，转换格式中可能部分适用：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `resolution` | string | `720p` | 生成视频分辨率，枚举：`480p`、`720p`、`1080p` |
| `seed` | integer | `-1` | 种子整数，[-1, 2^32-1] |
| `generate_audio` | boolean | `true` | 是否生成有声视频 |
| `watermark` | boolean | `false` | 是否包含水印 |
| `callback_url` | string | - | 任务结果回调通知地址 |
| `return_last_frame` | boolean | `false` | 是否返回生成视频的尾帧图像（png） |
| `service_tier` | string | `default` | `default` 在线推理 / `flex` 离线推理（2.0 不支持离线） |
| `execution_expires_after` | integer | `172800` | 任务超时阈值（秒），范围 [3600, 259200] |
| `tools` | object[] | - | 工具列表，支持 `web_search` 联网搜索 |
| `safety_identifier` | string | - | 终端用户唯一标识符，≤ 64 字符 |

---

## 请求示例

### cURL

```bash
curl -X POST 'https://your-api-host/v1/videos' \
  -H 'Content-Type: application/json' \
  -H 'Authorization: sk-xxx' \
  -d '{
    "model": "doubao-seedance-2-0-260128",
    "prompt": "全程使用视频1的第一视角构图，全程使用音频1作为背景音乐。第一人称视角果茶宣传广告，seedance牌「苹苹安安」苹果果茶限定款；首帧为图片1，你的手摘下一颗带晨露的阿克苏红苹果，轻脆的苹果碰撞声；2-4 秒：快速切镜，你的手将苹果块投入雪克杯，加入冰块与茶底，用力摇晃，冰块碰撞声与摇晃声卡点轻快鼓点，背景音：「鲜切现摇」；4-6 秒：第一人称成品特写，分层果茶倒入透明杯，你的手轻挤奶盖在顶部铺展，在杯身贴上粉红包标，镜头拉近看奶盖与果茶的分层纹理；6-8 秒：第一人称手持举杯，你将图片2中的果茶举到镜头前（模拟递到观众面前的视角），杯身标签清晰可见，背景音「来一口鲜爽」，尾帧定格为图片2。背景声音统一为女生音色。",
    "metadata": {
      "content": [
        {
          "type": "image_url",
          "image_url": {
            "url": "https://ark-project.tos-cn-beijing.volces.com/doc_image/r2v_tea_pic1.jpg"
          },
          "role": "reference_image"
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "https://ark-project.tos-cn-beijing.volces.com/doc_image/r2v_tea_pic2.jpg"
          },
          "role": "reference_image"
        },
        {
          "type": "video_url",
          "video_url": {
            "url": "https://ark-project.tos-cn-beijing.volces.com/doc_video/r2v_tea_video1.mp4"
          },
          "role": "reference_video"
        },
        {
          "type": "audio_url",
          "audio_url": {
            "url": "https://ark-project.tos-cn-beijing.volces.com/doc_audio/r2v_tea_audio1.mp3"
          },
          "role": "reference_audio"
        }
      ],
      "generate_audio": true,
      "ratio": "16:9",
      "duration": 11,
      "watermark": true
    }
  }'
```

### Python

```python
import os
import time
from volcenginesdkarkruntime import Ark

client = Ark(
    base_url='https://ark.cn-beijing.volces.com/api/v3',
    api_key=os.environ.get("ARK_API_KEY"),
)

# 创建任务
create_result = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {
            "type": "text",
            "text": "你的提示词内容",
        },
        {
            "type": "image_url",
            "image_url": {"url": "https://example.com/image.jpg"},
            "role": "reference_image",
        },
        {
            "type": "video_url",
            "video_url": {"url": "https://example.com/video.mp4"},
            "role": "reference_video",
        },
        {
            "type": "audio_url",
            "audio_url": {"url": "https://example.com/audio.mp3"},
            "role": "reference_audio",
        },
    ],
    generate_audio=True,
    ratio="16:9",
    duration=11,
    watermark=True,
)
task_id = create_result.id

# 轮询查询任务状态
while True:
    get_result = client.content_generation.tasks.get(task_id=task_id)
    status = get_result.status
    if status == "succeeded":
        print(get_result)
        break
    elif status == "failed":
        print(f"Error: {get_result.error}")
        break
    else:
        print(f"Current status: {status}")
        time.sleep(30)
```

---

## 响应参数

### 创建任务响应

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 视频生成任务 ID，仅保存 7 天 |

### 任务完成响应（查询获取）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 任务 ID |
| `model` | string | 使用的模型 ID |
| `status` | string | 任务状态：`queued` / `running` / `succeeded` / `failed` / `expired` |
| `content.video_url` | string | 生成的视频地址 |
| `content.last_frame_url` | string | 尾帧图像地址（仅 `return_last_frame=true` 时返回） |
| `content.file_url` | string | 文件地址 |
| `usage.completion_tokens` | integer | 消耗的 token 数 |
| `usage.total_tokens` | integer | 总 token 数 |
| `framespersecond` | integer | 帧率 |
| `seed` | integer | 使用的种子值 |
| `service_tier` | string | 推理模式 |
| `execution_expires_after` | integer | 超时阈值 |
| `generate_audio` | boolean | 是否生成了有声视频 |
| `duration` | integer | 视频时长 |
| `ratio` | string | 宽高比 |
| `resolution` | string | 分辨率 |
| `draft` | boolean | 是否为样片模式 |

### 响应示例

```json
{
  "id": "cgt-20260414114820-*****",
  "model": "doubao-seedance-2-0-260128",
  "status": "succeeded",
  "content": {
    "video_url": "https://...",
    "last_frame_url": null,
    "file_url": null
  },
  "usage": {
    "completion_tokens": 411300,
    "total_tokens": 411300
  },
  "framespersecond": 24,
  "seed": 33608,
  "service_tier": "default",
  "execution_expires_after": 172800,
  "generate_audio": true,
  "duration": 11,
  "ratio": "16:9",
  "resolution": "720p",
  "draft": false
}
```

---

## 接口格式对比

本项目中 Seedance 2.0 存在两种接口格式：

| 维度 | 官方格式（本文档） | 火山引擎原生格式 |
|------|-------------------|-----------------|
| 路径 | `/v1/videos` | `/api/v3/contents/generations/tasks` |
| prompt | 顶层 `prompt` 字段 | 在 `content[]` 中以 `type: "text"` 形式传入 |
| 素材 | 嵌套在 `metadata.content[]` 中 | 顶层 `content[]` 数组 |
| 生成参数 | 嵌套在 `metadata` 中（ratio, duration 等） | 顶层字段 |

> 详细的火山引擎原生格式文档请参阅 `接口文档/提克seedance2视频文档/` 目录。

---

## 兼容性分析与实现记录

> 更新日期：2026-05-12

### 兼容性总览

系统对 Seedance 2.0 接口的兼容度为 **90%+**，核心功能均已实现。

### 已兼容部分（无需修改）

| 能力 | 说明 |
|------|------|
| 路由 `POST /v1/videos` | `video-router.go` 已注册 |
| 路由 `GET /v1/videos/:task_id` | `video-router.go` 已注册 |
| 路由 `GET /v1/videos/:task_id/content` | 视频代理下载 |
| 模型列表（6 个） | `doubao-seedance-1-0-pro-250528`、`1-0-lite-t2v`、`1-0-lite-i2v`、`1-5-pro-251215`、`2-0-260128`、`2-0-fast-260128` |
| 上游调用 | 正确转换到火山引擎原生 `/api/v3/contents/generations/tasks` |
| 鉴权 Bearer Token | 适配器自动附加 |
| content 数组结构 | `ContentItem` 支持 `image_url` / `video_url` / `audio_url` + `role` |
| 素材角色 role 透传 | `first_frame` / `last_frame` / `reference_image` / `reference_video` / `reference_audio` |
| 文生视频 | 仅 prompt + 参数 |
| 首帧图生视频 | content 中传 `first_frame` |
| 首尾帧模式 | content 中传 `first_frame` + `last_frame` |
| 多图参考 | content 中传多个 `reference_image` |
| 多模态模式 | content 中混合 `reference_image` / `reference_video` / `reference_audio` |
| 核心生成参数 | `resolution`、`ratio`、`duration`、`generate_audio`、`watermark`、`seed`、`camera_fixed`、`return_last_frame` |
| 高级参数 | `service_tier`、`execution_expires_after`、`callback_url`、`draft`、`tools` |
| 两种传参方式 | 顶层直接传参（方式1）和 `metadata` 嵌套传参（方式2）均支持 |
| 视频输入计费折扣 | `2-0-260128`: 28/46 ≈ 0.6087, `2-0-fast-260128`: 22/37 ≈ 0.5946 |
| 任务状态轮询 | 15 秒周期，`ParseTaskResult` 映射上游 status |
| Usage 解析 | `completion_tokens` / `total_tokens` 从上游响应解析，用于按量计费 |

### 本次新增/修复内容

#### 1. 查询响应补充丰富 metadata 字段

**文件**：`relay/channel/task/doubao/adaptor.go` — `ConvertToOpenAIVideo()`

**变更前**：查询响应 `GET /v1/videos/{task_id}` 的 `metadata` 仅包含 `url`（视频地址）。

**变更后**：metadata 中新增以下上游返回的字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `url` | string | 生成的视频地址（已有） |
| `seed` | int | 使用的种子值（非零时返回） |
| `duration` | int | 实际视频时长秒数（大于 0 时返回） |
| `ratio` | string | 宽高比，如 `16:9`（非空时返回） |
| `resolution` | string | 分辨率，如 `720p`（非空时返回） |
| `framespersecond` | int | 帧率（大于 0 时返回） |
| `service_tier` | string | 推理模式 `default` / `flex`（非空时返回） |
| `generate_audio` | bool | 是否生成了有声视频（始终返回） |
| `draft` | bool | 是否为样片模式（始终返回） |
| `usage` | object | `{completion_tokens, total_tokens}`（有值时返回） |

**变更后查询响应示例**：

```json
{
  "id": "task_brid81VeF1emGAQjL5cAp73fFSDymUyI",
  "task_id": "task_brid81VeF1emGAQjL5cAp73fFSDymUyI",
  "object": "video",
  "model": "doubao-seedance-2-0-260128",
  "status": "completed",
  "progress": 100,
  "created_at": 1777347208,
  "completed_at": 1777347613,
  "metadata": {
    "url": "https://ark-acg-cn-beijing.tos-cn-beijing.volces.com/...",
    "seed": 33608,
    "duration": 11,
    "ratio": "16:9",
    "resolution": "720p",
    "framespersecond": 24,
    "service_tier": "default",
    "generate_audio": true,
    "draft": false,
    "usage": {
      "completion_tokens": 411300,
      "total_tokens": 411300
    }
  }
}
```

#### 2. 新增 `expired` 状态映射

**文件**：`relay/channel/task/doubao/adaptor.go` — `ParseTaskResult()`

**变更前**：上游返回 `expired` 状态时走 default 分支，被误判为 `in_progress`。

**变更后**：`expired` 正确映射为 `TaskStatusFailure`，reason 设为 `"task expired"`，对外返回 `status: "failed"`。

**完整状态映射表**：

| 上游状态 | 内部状态 | 对外 Video Status | Progress |
|----------|----------|-------------------|----------|
| `pending` / `queued` | `queued` | `queued` | 10% |
| `processing` / `running` | `in_progress` | `in_progress` | 50% |
| `succeeded` | `success` | `completed` | 100% |
| `failed` | `failure` | `failed` | 100% |
| `expired` | `failure` | `failed` | 100% |
| 其他未知 | `in_progress` | `in_progress` | 30% |

#### 3. `responseTask` 结构体补全

**文件**：`relay/channel/task/doubao/adaptor.go` — `responseTask` struct

新增 `GenerateAudio bool` 和 `Draft bool` 字段，对齐火山引擎上游完整响应结构。
