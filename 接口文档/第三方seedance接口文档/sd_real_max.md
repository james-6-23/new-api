# 素材（SD Assets）与视频生成 API 文档

base_url = https://model.service-inference.ai


## 通用约定

- **BASE_URL**：服务地址，下文以 `$BASE_URL` 表示。
- **API_KEY**：鉴权密钥，下文以 `$API_KEY` 表示，通过 `Authorization: Bearer $API_KEY` 传递。
- 除特别说明外，请求与响应均为 JSON（`Content-Type: application/json`）。
- 响应中 `base_resp.status_code` 为 `0` 表示成功。

```bash
export BASE_URL="https://<your-endpoint>"
export API_KEY="<your-api-key>"
```

---

## 1. 上传素材

创建一个素材（图片 / 视频 / 音频），供后续视频生成引用。

**请求**

```bash
curl --location "$BASE_URL/v1/sd/assets" \
--header "Authorization: Bearer $API_KEY" \
--header "Content-Type: application/json" \
--data '{
    "URL": "https://example.com/your-image.jpg",
    "Name": "avatar_front",
    "AssetType": "Image"
  }'
```

**参数说明**

| 字段        | 说明                                   |
| ----------- | -------------------------------------- |
| `URL`       | 素材的源地址                           |
| `Name`      | 素材名称                               |
| `AssetType` | 素材类型：`Image` / `Video` / `Audio`  |

**响应**

```json
{
    "success": true,
    "data": {
        "Id": "asset-20260705003737-njxmg",
        "base_resp": {
            "status_code": 0,
            "status_msg": "success"
        }
    }
}
```

返回的 `data.Id` 即素材 ID，可在生成视频时通过 `asset://<Id>` 引用。

---

## 2. 查询素材

根据素材 ID 查询素材详情。

**请求**

```bash
curl --location --request GET "$BASE_URL/v1/sd/assets/asset-20260704201533-vv2bh" \
--header "Authorization: Bearer $API_KEY" \
--header "Content-Type: application/json"
```

**响应**

```json
{
    "success": true,
    "data": {
        "Id": "asset-20260704201533-vv2bh",
        "Status": "Active",
        "AssetType": "Image",
        "Name": "avatar_front",
        "URL": "<asset-url>",
        "GroupId": null,
        "CreateTime": "2026-07-04T12:15:34Z",
        "UpdateTime": "2026-07-04T12:15:36Z",
        "base_resp": {
            "status_code": 0,
            "status_msg": "success"
        }
    }
}
```

---

## 3. 生成视频

提交一个视频生成任务，接口立即返回任务信息，需后续轮询任务状态获取结果。
model id 只支持hc 模型，用上面素材接口提交的话
**请求**

```bash
curl --location "$BASE_URL/v1/video/generate" \
--header "Authorization: Bearer $API_KEY" \
--header "Content-Type: application/json" \
--data '{
  "model": "dreamina-seedance-2-0-hc",
  "content": [
    {
      "type": "text",
      "text": "这个人在跳舞"
    },
    {
      "type": "image_url",
      "image_url": {
        "url": "asset://asset-x-5d79l"
      },
      "role": "reference_image"
    },
    {
      "type": "video_url",
      "video_url": {
        "url": "asset://asset-x-5d79l"
      },
      "role": "reference_video"
    },
    {
      "type": "audio_url",
      "audio_url": {
        "url": "https://xxx"
      },
      "role": "reference_audio"
    }
  ],
  "duration": 5,
  "resolution": "480p",
  "ratio": "1:1",
  "generate_audio": false,
  "watermark": false
}'
```

**参数说明**

| 字段             | 说明                                             |
| ---------------- | ------------------------------------------------ |
| `model`          | 视频生成模型                                     |
| `content`        | 输入内容数组，支持文本（`text`）与图片（`image_url,video_url,audio_url`）|
| `image_url.url`  | 可用 `asset://<素材ID>` 引用已上传素材            |
| `role`           | 图片角色，如 `reference_image`（参考图）          |
| `duration`       | 视频时长（秒）                                   |
| `resolution`     | 分辨率，如 `480p`                                |
| `ratio`          | 画面比例，如 `1:1`                               |
| `generate_audio` | 是否生成音频                                     |
| `watermark`      | 是否添加水印                                     |

**响应**

```json
{
    "task": {
        "id": "mvt-475330e24c864ec6",
        "status": "pending",
        "model": "dreamina-seedance-2-0-hc",
        "duration_seconds": 5,
        "outputs": [],
        "error": null,
        "created_at": "2026-07-04T16:41:41.705Z",
        "completed_at": null
    }
}
```

记录返回的 `task.id`，用于后续查询任务状态。

---

## 4. 查询任务列表

获取所有视频生成任务的列表（可用于批量查看进度）。

**请求**

```bash
curl --location "$BASE_URL/v1/video/tasks" \
--header "Authorization: Bearer $API_KEY" \
--header "Content-Type: application/json"
```

**响应**

```json
{
    "tasks": [
        {
            "id": "mvt-179197ccca01401a",
            "status": "completed",                   // 任务状态：已完成
            "model": "dreamina-seedance-2-0-260128",
            "duration_seconds": 4,
            "outputs": [
                "<video-download-url>"               // 生成好的视频下载地址
            ],
            "error": null,
            "created_at": "2026-05-26T05:26:52.505Z",
            "completed_at": "2026-05-26T05:35:22.566Z",
            "usage": {
                "completion_tokens": 40594,          // 消耗的 token 数（计费用）
                "total_tokens": 40594
            },
            "last_frame_url": "$BASE_URL/v1/video/files/mvt-5ec29e7e2db74b62/last-frame"  // 最后一帧图片地址
        }
    ],
    "total": 1,           // 任务总数
    "totalPages": 1       // 总页数
}
```

---

## 5. 查询单个任务详情

用 `task.id` 查询指定任务的最新状态和结果（轮询时最常用）。

**请求**

```bash
# URL 末尾拼上任务 id
curl --location "$BASE_URL/v1/video/tasks/mvt-179197ccca01401a" \
--header "Authorization: Bearer $API_KEY"
```

**响应**（`status` 为 `completed` 时，从 `outputs` 取视频地址）

```json
{
    "task": {
        "id": "mvt-179197ccca01401a",
        "status": "completed",
        "model": "dreamina-seedance-2-0-260128",
        "duration_seconds": 4,
        "outputs": [
            "<video-download-url>"                   // 生成好的视频地址
        ],
        "error": null,
        "created_at": "2026-05-26T05:26:52.505Z",
        "last_frame_url": "$BASE_URL/v1/video/files/mvt-5ec29e7e2db74b62/last-frame",
        "completed_at": "2026-05-26T05:35:22.566Z",
        "usage": {
            "completion_tokens": 40594,
            "total_tokens": 40594
        }
    }
}
```

**任务状态（status）说明**

status = ["completed", "failed", "processing"]