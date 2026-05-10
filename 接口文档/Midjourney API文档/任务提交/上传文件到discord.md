# 上传文件到discord

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/submit/upload-discord-images:
    post:
      summary: 上传文件到discord
      deprecated: false
      description: 上传文件到discord。
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
                base64Array:
                  type: array
                  items:
                    type: string
                  description: base64数组
              required:
                - base64Array
                - mode
              x-apifox-orders:
                - mode
                - base64Array
            example:
              mode: RELAX
              base64Array:
                - data:image/png;base64,xxx1
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
                    description: >-
                      状态码: 1(提交成功), 22(排队中),
                      23(队列已满，请稍后尝试),24(prompt包含敏感词),other(错误)
                  description:
                    type: string
                    description: 描述
                  result:
                    type: array
                    items:
                      type: string
                    description: 图片地址
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
                description: success
                result:
                  - >-
                    https://cdn.discordapp.com/attachments/1235333617467789395/1237020337057828935/1508569645328691200.jpg?ex=663a2077&is=6638cef7&hm=991c97a66f00ee5b5714848ccf0cded22929d58f0d809f0057c71c4b29ee469d&
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务提交
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359301-run
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
