# 提交Shorten任务

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/submit/shorten:
    post:
      summary: 提交Shorten任务
      deprecated: false
      description: 执行`Shorten`操作。
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
                  description: 提示词,示例值(Cat)
                botType:
                  type: string
                  description: 'bot类型，`mj`: MID_JOURNEY, `niji`: NIJI_JOURNEY'
                notifyhook:
                  type: string
                  description: 回调地址，为空时不进行回调通知
                state:
                  type: string
                  description: 自定义参数
              required:
                - prompt
                - mode
              x-apifox-orders:
                - mode
                - prompt
                - botType
                - notifyhook
                - state
            example:
              mode: RELAX
              notifyHook: ''
              prompt: Cat
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
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359298-run
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
