# 提交SwapFace任务

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/insight-face/swap:
    post:
      summary: 提交SwapFace任务
      deprecated: false
      description: 提交`SwapFace`任务，进行换脸操作。
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
          multipart/form-data:
            schema:
              type: object
              properties:
                mode:
                  description: '调用模式，默认RELAX，`RELAX`: 慢速模式，`FAST`: 快速模式'
                  example: ''
                  type: string
                source:
                  format: binary
                  type: string
                  description: 人脸源图片
                  example: ''
                target:
                  format: binary
                  type: string
                  description: 目标图片
                  example: ''
            example:
              mode: RELAX
              notifyHook: ''
              sourceBase64: data:image/png;base64,xxx1
              targetBase64: data:image/png;base64,xxx2
              state: ''
      responses:
        '200':
          description: ''
          content:
            application/json:
              schema:
                type: object
                properties:
                  code:
                    type: integer
                    description: '状态码: 1-提交成功，22-排队中，23-队列已满，请稍后尝试，24-prompt包含敏感词，other-错误'
                  description:
                    type: string
                    description: 描述
                  result:
                    type: string
                    description: 任务ID
                required:
                  - code
                  - description
                  - result
                x-apifox-orders:
                  - code
                  - description
                  - result
              example:
                code: 1
                description: Submit success
                result: '1712211887200849'
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务提交
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359299-run
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
