# 提交Video任务

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/submit/video:
    post:
      summary: 提交Video任务
      deprecated: false
      description: ''
      tags:
        - 图像模型/Midjourney API文档/任务提交
      parameters:
        - name: Authorization
          in: header
          description: ''
          example: '{{Authorization}}'
          schema:
            type: string
            default: '{{Authorization}}'
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                mode:
                  type: string
                  description: '调用模式，默认RELAX，`RELAX`: 慢速模式，`FAST`: 快速模式'
                prompt:
                  type: string
                  description: "提示词,示例值(Cat)\t"
                taskId:
                  type: string
                  description: 需要操作的视频父任务ID
                index:
                  type: integer
                  description: 执行的视频索引号
                motion:
                  type: string
                  enum:
                    - low
                    - high
                  x-apifox:
                    enumDescriptions:
                      low: ''
                      high: ''
                image:
                  type: string
                  description: 首帧图片，扩展时可为空
                  enum:
                    - url
                    - base64
                  x-apifox:
                    enumDescriptions:
                      url: ''
                      base64: ''
                action:
                  type: string
                  description: 对视频任务进行操作。不为空时，index、taskld必填
                  enum:
                    - extend
                  x-apifox:
                    enumDescriptions:
                      extend: ''
                state:
                  type: string
                notifyHook:
                  type: string
                  description: 回调地址
              required:
                - mode
                - taskId
                - index
                - motion
              x-apifox-orders:
                - mode
                - prompt
                - motion
                - taskId
                - index
                - image
                - action
                - state
                - notifyHook
            example:
              mode: FAST
              prompt: a car
              taskId: '1712204995849323'
              index: 1
      responses:
        '200':
          description: ''
          content:
            application/json:
              schema:
                type: object
                properties: {}
                x-apifox-orders: []
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务提交
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359300-run
components:
  schemas: {}
  securitySchemes:
    bearer:
      type: bearer
      scheme: bearer
    BearerAuth:
      type: bearer
      scheme: bearer
      bearerFormat: API Key
    BearerAuth1:
      type: bearer
      scheme: bearer
      bearerFormat: API Key
servers: []
security: []

```
