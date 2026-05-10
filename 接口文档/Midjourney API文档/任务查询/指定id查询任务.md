# 指定id查询任务

## OpenAPI Specification

```yaml
openapi: 3.0.1
info:
  title: ''
  description: ''
  version: 1.0.0
paths:
  /mj/task/{id}/fetch:
    get:
      summary: 指定id查询任务
      deprecated: false
      description: 通过任务`id`，查询任务信息。（可以通过轮询调用该接口，实现任务进行的查询。也可以通过回调接口获取）
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
                  id:
                    type: string
                    description: ID 编号
                  action:
                    type: string
                    description: >-
                      任务类型,可用值:`IMAGINE`,`UPSCALE`,`VARIATION`,`ZOOM`,`PAN`,`DESCRIBE`,`BLEND`,`SHORTEN`,`SWAP_FACE`
                  prompt:
                    type: string
                    description: 提示词
                  promptEn:
                    type: string
                    description: 提示词-英文
                  description:
                    type: string
                    description: 任务描述
                  submitTime:
                    type: integer
                    description: 提交时间
                  startTime:
                    type: integer
                    description: 开始执行时间
                  finishTime:
                    type: integer
                    description: 结束时间
                  imageHeight:
                    type: integer
                    description: 图片高度
                  imageWidth:
                    type: integer
                    description: 图片宽度
                  imageUrl:
                    type: string
                    description: 图片url
                  VideoUrl:
                    type: string
                    description: 视频url
                  status:
                    type: string
                    description: >-
                      任务状态,可用值:`NOT_START`,`SUBMITTED`,`MODAL`,`IN_PROGRESS`,`FAILURE`,`SUCCESS`,`CANCEL`
                  progress:
                    type: string
                    description: 任务进度
                  failReason:
                    type: string
                    description: 失败原因
                  buttons:
                    type: array
                    items:
                      $ref: '#/components/schemas/MjButton'
                    description: 按钮数组：图片下方对应的各个按钮数组，需要点击按钮的时候，把customId传给action接口即可
                    nullable: true
                  state:
                    type: string
                    description: 自定义参数
                  imageUrls:
                    type: array
                    items:
                      type: string
                    description: 图片URL列表
                required:
                  - id
                  - action
                  - prompt
                  - promptEn
                  - description
                  - submitTime
                  - startTime
                  - finishTime
                  - imageHeight
                  - imageWidth
                  - imageUrl
                  - VideoUrl
                  - status
                  - progress
                  - imageUrls
                x-apifox-orders:
                  - id
                  - action
                  - prompt
                  - promptEn
                  - description
                  - submitTime
                  - startTime
                  - finishTime
                  - imageHeight
                  - imageWidth
                  - imageUrl
                  - VideoUrl
                  - status
                  - progress
                  - failReason
                  - buttons
                  - state
                  - imageUrls
                x-apifox-ignore-properties: []
          headers: {}
          x-apifox-name: 成功
      security: []
      x-apifox-folder: 图像模型/Midjourney API文档/任务查询
      x-apifox-status: released
      x-run-in-apifox: https://app.apifox.com/web/project/6149777/apis/api-375359302-run
components:
  schemas:
    MjButton:
      type: object
      properties:
        customId:
          type: string
          description: 动作标识
        emoji:
          type: string
          description: 图标
        label:
          type: string
          description: 文本
        type:
          type: integer
          description: '样式: 2（Primary）、3（Green）'
        style:
          type: integer
          description: 类型，系统内部使用
      x-apifox-orders:
        - customId
        - emoji
        - label
        - type
        - style
      x-apifox-ignore-properties: []
      x-apifox-folder: ''
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
