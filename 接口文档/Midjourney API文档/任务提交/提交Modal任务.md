# 提交Modal任务

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/submit/modal:
    post:
      summary: 提交Modal任务
      deprecated: false
      description: 当执行其他任务，`code`返回`21`时，需要执行`modal`接口，传入新的提示词用来修改细节。
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
                taskId:
                  type: string
                  description: "任务ID,示例值(14001934816969359)\t"
                maskBase64:
                  type: string
                  description: 局部重绘的蒙版base64，该字段用于局部重绘时传入
                prompt:
                  type: string
                  description: 提示词，大部分时候都需要传
              required:
                - taskId
              x-apifox-orders:
                - taskId
                - maskBase64
                - prompt
            example:
              maskBase64: data:image/png;base64,xxx1
              prompt: Cat
              taskId: '1712204995849323'
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
                result: '1712204995849323'
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务提交
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359296-run
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
