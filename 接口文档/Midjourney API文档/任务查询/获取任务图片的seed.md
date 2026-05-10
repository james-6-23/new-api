# 获取任务图片的seed

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/task/{id}/image-seed:
    get:
      summary: 获取任务图片的seed
      deprecated: false
      description: 通过任务id，查询图片`seed`。
      tags:
        - 图像模型/Midjourney API文档/任务查询
      parameters:
        - name: id
          in: path
          description: 任务ID
          required: true
          example: ''
          schema:
            type: string
        - name: Authorization
          in: header
          description: ''
          example: '{{Authorization}}'
          schema:
            type: string
            default: '{{Authorization}}'
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
                    type: string
                    description: 图片seed
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
                description: ''
                result: ''
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务查询
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359304-run
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
