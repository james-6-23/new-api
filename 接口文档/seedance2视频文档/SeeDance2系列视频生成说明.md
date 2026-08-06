# SeeDance2系列视频生成说明

# 必看

### **当前doubao-seedance-2.0系列 default分组不支持虚拟人像/真人接口.**  
### **sd-svip分组支持虚拟人像生成视频**.
### **均不支持活体人像检测**

## 当前已兼容以下 Seedance 2.0 输入场景：

- 多模态模式：图片、视频、音频输入
- 多图参考模式
- 首帧模式
- 首尾帧模式
- 纯文生视频
- 官方文档
- 视频生成 https://www.volcengine.com/docs/82379/1520757?lang=zh
- 视频查询 https://www.volcengine.com/docs/82379/1521309?lang=zh
- 视频生成教程  https://www.volcengine.com/docs/82379/2298881?redirect=1&lang=zh
- Doubao Seedance 2.0 系列教程 https://www.volcengine.com/docs/82379/2291680?lang=zh
## 支持的请求方式

当前支持两种传参方式：

1. 顶层直接传火山官方参数
2. 将火山原始参数放入 `metadata`

推荐优先使用方式 1，和火山官方文档更接近。
说明:中文提示词不超过500字，英文提示词不超过1000词。

---

## 方式 1：推荐写法

### 1. 文生视频

```json
{
  "model": "doubao-seedance-2-0-fast-260128",
  "prompt": "一个女孩跳舞",
  "resolution": "480p",
  "ratio": "9:16",
  "duration": 5,
  "generate_audio": true,
  "watermark": false
}
```

### 2. 单图参考 / 首帧模式

```json
{
  "model": "doubao-seedance-2-0-fast-260128",
  "prompt": "让人物自然眨眼并轻微转头",
  "content": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": {
        "url": "https://example.com/start.png"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "watermark": false
}
```

### 3. 首尾帧模式

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "镜头平稳推进，人物表情自然变化",
  "content": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": {
        "url": "https://example.com/start.png"
      }
    },
    {
      "type": "image_url",
      "role": "last_frame",
      "image_url": {
        "url": "https://example.com/end.png"
      }
    }
  ],
  "return_last_frame": true,
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "watermark": false
}
```

### 4. 多图参考模式

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "融合多张参考图中的人物与场景气质，生成电影感短视频",
  "content": [
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://example.com/ref1.png"
      }
    },
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://example.com/ref2.png"
      }
    },
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://example.com/ref3.png"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5
}
```

### 5. 多模态模式：图 + 视频 + 音频参考

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "保持人物主体一致，参考视频运动节奏和参考音频氛围",
  "content": [
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "https://example.com/portrait.png"
      }
    },
    {
      "type": "video_url",
      "role": "reference_video",
      "video_url": {
        "url": "https://example.com/reference.mp4"
      }
    },
    {
      "type": "audio_url",
      "role": "reference_audio",
      "audio_url": {
        "url": "https://example.com/reference.mp3"
      }
    }
  ],
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "generate_audio": false,
  "watermark": false
}
```

---



## 方式 2：兼容写法

如果你希望完全把火山原始参数包进 `metadata`，也支持：

```json
{
  "model": "doubao-seedance-2-0-fast-260128",
  "prompt": "一个女孩跳舞",
  "metadata": {
    "content": [
      {
        "type": "image_url",
        "role": "reference_image",
        "image_url": {
          "url": "https://example.com/ref.png"
        }
      }
    ],
    "resolution": "480p",
    "generate_audio": true,
    "ratio": "9:16",
    "duration": 5,
    "watermark": false
  }
}
```

---

## 已兼容的主要参数

当前已接入并会透传到火山上游的主要字段如下：

- `model`
- `prompt`
- `content`

- `safety_identifier`
- `return_last_frame`
- `service_tier`
- `execution_expires_after`
- `generate_audio`
- `resolution`
- `ratio`
- `duration`
- `seed`
- `camera_fixed`
- `watermark`


`content` 中已支持的素材类型：

- `text`
- `image_url`
- `video_url`
- `audio_url`

`content[].role` 当前不会被网关拦截，会按请求值透传。常见可用值包括：

- `reference_image`
- `reference_video`
- `reference_audio`
- `first_frame`
- `last_frame`

---

### 3. 顶层参数与 `metadata` 同时传时

当前兼容逻辑下：

- `metadata` 中已有的同名字段优先
- 顶层字段用于补全 `metadata` 中缺失的同名字段

因此不建议同一个参数在顶层和 `metadata` 里传两份不同值。


## 当前推荐

如果你在业务侧直接接 Seedance 2.0，建议统一采用：

- 顶层 `content`
- 顶层 `resolution / ratio / duration / generate_audio / watermark`
- 需要图片参考时使用标准 `image_url`
- 需要首尾帧时显式使用 `first_frame` / `last_frame`
