```
curl --location 'https://model.service-inference.ai/v1/asset-groups' \
--header 'Authorization: Bearer x' \
--header 'Content-Type: application/json' \
--data '{
    "name": "lifeng",
    "description": "lifeng test"
}'
```
```
{
    "id": "group-xxxx-99w5l",
    "name": "lifeng",
    "description": "lifeng test"
}
```


```
curl --location 'https://model.service-inference.ai/v1/asset-groups/{gourp-id}' \
--header 'Authorization: Bearer sk-inf-v1-0f1a7f6d81606caa9e93db1cb77424eca400c54a73463f0c1175e585a12b08bb' \
--header 'Content-Type: application/json'
```
```
{
    "id": "group-x-x",
    "name": "child",
    "title": "",
    "description": "",
    "group_type": "AIGC",
    "project_name": "default",
    "created_at": "2026-06-01T23:52:23Z",
    "updated_at": "2026-06-01T23:52:23Z"
}
```

我们这边会轮转素材组，如果没找到话，需要重新创建

```
curl --location 'https://model.service-inference.ai/v1/assets' \
--header 'Authorization: Bearer x' \
--header 'Content-Type: application/json' \
--data '{
    "group_id": "group-xxx-99w5l",
    "url": "{url}",
    "asset_type": "Image",
    "name": "child"
}'
```

```
{
    "id": "asset-xx-pvcn4",
    "task_id": "xxx",
    "status": "processing"
}
```


```
curl --location 'https://model.service-inference.ai/v1/assets/get' \
--header 'Authorization: Bearer x' \
--header 'Content-Type: application/json' \
--data '{
    "asset_id": "asset-",
    "task_id": "task_9x"
}'
```
```
{
    "id": "asset-xxxxxx-pvcn4",
    "name": "child",
    "url": "x,
    "asset_type": "Image",
    "group_id": "group-xxx-99w5l",
    "status": "completed",
    "error": null,
    "created_at": "2026-05-26T05:24:58Z",
    "updated_at": "2026-05-26T05:25:03Z"
}
```


```
curl --location 'https://model.service-inference.ai/v1/video/generate' \
--header 'Authorization: Bearer x' \
--header 'Content-Type: application/json' \
--data '{
    "model": "dreamina-seedance-2-0-260128",
    "content": [
         {
            "type": "text",
            "text": "这个哥们在唱歌"
        },
        {
            "type": "image_url",
            "image_url": {
            "url": "asset://x"            
        },
            "role": "reference_image"
        },
        {
          "type": "audio_url",
          "audio_url": {
              "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_audio/r2v_tea_audio1.mp3"
          },
          "role": "reference_audio"
        }
    ],
      "duration": 4,
      "resolution": "480p",
      "ratio": "16:9",
      "generate_audio": true,
      "watermark": false,
      "return_last_frame":true

    
}'

```
{
    "task": {
        "id": "mvt-179197ccca01401a",
        "status": "pending",
        "model": "dreamina-seedance-2-0-260128",
        "duration_seconds": 4,
        "outputs": [],
        "error": null,
        "created_at": "2026-05-26T05:26:52.505Z",
        "completed_at": null
    }
}
```



```
curl --location 'https://model.service-inference.ai/v1/video/tasks' \
--header 'Authorization: Bearer x' \
--header 'Content-Type: application/json'
```

```
{
    "tasks": [
        {
            "id": "mvt-179197ccca01401a",
            "status": "completed",
            "model": "dreamina-seedance-2-0-260128",
            "duration_seconds": 4,
            "outputs": [
                "https://xx"
            ],
            "error": null,
            "created_at": "2026-05-26T05:26:52.505Z",
            "completed_at": "2026-05-26T05:35:22.566Z",
            "usage": {
                "completion_tokens": 40594,
                "total_tokens": 40594
            },
            "last_frame_url": "https://model.service-inference.ai/v1/video/files/mvt-5ec29e7e2db74b62/last-frame"

            
        }
    ],
    "total": 1,
    "totalPages": 1
}
```

```
curl --location 'https://model.service-inference.ai/v1/video/tasks/mvt-179197ccca01401a' \
--header 'Authorization: Bearer x' \
```

```
{
    "task": {
        "id": "mvt-179197ccca01401a",
        "status": "completed",
        "model": "dreamina-seedance-2-0-260128",
        "duration_seconds": 4,
        "outputs": [
            "https://xxx"
        ],
        "error": null,
        "created_at": "2026-05-26T05:26:52.505Z",
        "last_frame_url": "https://model.service-inference.ai/v1/video/files/mvt-5ec29e7e2db74b62/last-frame",

        "completed_at": "2026-05-26T05:35:22.566Z",
        "usage": {
            "completion_tokens": 40594,
            "total_tokens": 40594
        }
    }
}
```