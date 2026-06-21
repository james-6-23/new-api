# 【对客】BytePlus 素材库关闭 content filter 操作说明

Seedancce2\.0国内外api可单独外发汇总：[seedance2\.0\-对外api文档](https://bytedance.larkoffice.com/docx/UEzLdctgToqNt5xTNQvcGLRfnKb)

关于下审核：

仅限海外api，需要关闭素材库上传 \+ 模型 两方面的限制，注意版权ip 明星等无法关闭。

1、素材库已默认关闭护栏操作，api传入Skip值即可。

2、用户参考关闭模型推理点护栏即可，model传入自定义推理点id

详细参考以下示例教程：

不同账户隔离、不同项目隔离，国内外平台隔离。

上传素材库后，模型调用必须使用asset id，否则不走素材库。

1、素材库：关闭拦截只是黄暴政，明星版权IP仍会拦截的。 素材库的作用就是为了上传真人脸，非人脸无需上传素材库，模型直接调用即可；

2、模型： 强限制不能上传真人脸。真人脸必须上传素材库后使用asset id。 关闭审核是关闭黄暴政，明星版权还是会拦截的。

3、素材库get接口的素材公网url\(12h有效期\)就是相当于自行上传图片，模型侧还是会拦截的，需要使用asset id。公网url除了人脸，其它的理论上发给谁都可以用，因为模型不拦截。

# 关闭素材库上传审核\-6月新操作方式

> ## **说明**
> 
> **主账号无需操作关闭 secure mode ，直接api调用传参即可。**
> 
> 

- 一句话说明使用方式：

    - 控制台：支持客户直接在 **控制台创建 asset group / asset 时** 通过  **Content pre\-filter 开关****，**

    - **api：在 ****CreateAsset API **中通过 **moderation 参数**** **决定**本次请求采用基线审核还是底线审核。**

    （**"Moderation": \{**
    **      "Strategy": "Skip"  \-\-关闭审核**
    **  \}**）

    - 对于**历史已关闭secure mode的用户**，若图片/视频内容属于在**关闭审核**时上传的素材，则**展示缺省页**，避免违规内容直接透出【注：由于刷数可能会有少量误差，可能存在非关闭审核时上传内容，但显示缺省页的情况】

- **控制台变化：**

    - 移除 secure mode 关闭入口；恢复历史关闭账号的素材列表查看能力。

- **接口变化：**

    - CreateAsset API 中 moderation 参数**不再依赖 secure mode 状态**即可生效。

- **官方API文档：**

    - CreateAsset API 文档：https://docs\.byteplus\.com/en/docs/ModelArk/2318271

    - 虚拟人像库教程：https://docs\.byteplus\.com/en/docs/ModelArk/2333565

    - 真人认证教程：https://docs\.byteplus\.com/en/docs/ModelArk/2333589

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=Y2NhMGI1YWQ1YjExYTM4YzNiZTU3YjI0MjZmOWM3OTRfN2ZmZjBkNjA2MzJlYmFlODYwMzVhMjAzNmVkZGU2NzJfSUQ6NzY1MTU1ODUzNTc0Njg0OTcyMF8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)



![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=MjRiOTU1NTQ5YjRkNjk4N2RkYTFjZmVlMDEzM2I4YTlfYWM4OTBhMjdkZmZhNGQ5Y2M4YzRjNGQxNmE5NWY3NDlfSUQ6NzY1MTU1NjY1NzYzMTU2MjczMl8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

## \*重要\-必看\*上传素材API时增加参数 [**Moderation**](https://docs.byteplus.com/en/docs/ModelArk/2318271)**：**

素材库api: https://docs\.byteplus\.com/en/docs/ModelArk/2318271

```JSON
{
  "GroupId": "group-2026**********-*****",
  "URL": "https://example.com/image.jpg",
  "AssetType": "Image",
  **"Moderation": {**
**      "Strategy": "Skip"  --关闭审核**
**  }**
}
```

# \*重要\-必看\*关闭视频模型审核【内容预过滤】一起使用：

**自定义在线推理点** 才支持关闭模型层面的安全护栏，预置推理点不支持关闭：

海外模型关闭护栏：创建在线自定义推理点时、选择模型后关闭content filter

model="doubao\-seedance\-2\-0\-260128"走的是预置推理点，无法关闭护栏。

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=MDJmNmIwYzdjZmY0YjQzYjcyNWI4YTlkNmI3MjNmZDVfOWVjZWI3MjE0MzkyZGFmYjZiYWU3MGI3ZjE3NWNhNjVfSUQ6NzY1MTU1NzAwNTQ1MDQ2NDUwMV8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

#### 新建推理点时关闭护栏

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=MmVhOWFmNTM5Y2IyNDQ2ZmJkMDEyOGVlZjA2OWMzMmFfOTlkYTk3NzVkZGQwODNhZWE3N2UzZmY1ZTU0NzEzZDVfSUQ6NzY1MTU1NzAwNzEwNjk1MjE2Nl8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

#### 或编辑已有推理点关闭护栏：on是默认开始，需要关闭

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=ZThjYmI5MmI1NDBkMWVjYTZkYzBiMzk4OGFiNzgxZDBfYzdiZjFkN2E0Y2M4ZDBmNzU1YmRjZGRjNjNmNTBhMzBfSUQ6NzY1MTU1NzAwNjYyMDYyNTg4NF8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

#### 如何调用模型

使用模型时，model 需填写对应的推理点id，如：ep\-xxxx\-xxx

model="doubao\-seedance\-2\-0\-260128"走的是预置推理点，无法关闭护栏。

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=Yzg5ZGZkYjNlMmEyYjEyNTI3Y2JmZmNkYjY2OTRiZWVfNWFlNzE0ODAyZTAwOWQ2N2Y4ZGJiY2MwYjRiOWExZTJfSUQ6NzY1MTU1NzAwNDkzMDc0NzMzMF8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

# 关闭素材库上传护栏限制

素材库由管理员权限操作统一关闭即可。

## ~~（旧方法\-已下线）在控制台关闭【安全模式】，注意此操作~~**~~不可逆，操作后，就无法在控制台查看和管理素材，只能通过 ~~**[**~~API 操作~~**](https://docs.byteplus.com/en/docs/ModelArk/2333565?redirect=1)**~~。~~**

~~采用“关闭 secure mode → 隐藏控制台素材能力 → API 传参决定是否跳过审核” ~~

~~该开关为账号级控制项，关闭后，当前账号下所有 IAM 子账号将~~**~~无法在控制台查看~~**~~素材内容，仅支持通过~~~~ ~~[**~~API~~**](https://docs.byteplus.com/en/docs/ModelArk/2333565?redirect=1)**~~ ~~**~~访问~~~~。操作前请务必通知 ModelArk 账号下所有 IAM 用户，确认业务影响后再执行关闭操作。~~

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=ZTA3YjRjMmQyOWRjYjUzMzYxYjMxNWU3MWJhNTg2MmVfZDVkNDIwNDQ1NzUyNDdlNDA0YjMxNjhiZjY5M2FiNDJfSUQ6NzY0Mzc3NDU2MDcxNDA2NzEyOV8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)

![Image](https://internal-api-drive-stream.larkoffice.com/space/api/box/stream/download/authcode/?code=NDE2NTUwMDU0NWVjYzc1ODk4OTA2ZDVkN2E5NzEzZmJfZjU2YjZjMDg4NTAxY2M1MjUwMWEwY2QyODY5MGFkZTBfSUQ6NzY0Mzc3NDU1OTQ1MTk5MTIxOF8xNzgxOTc4Mzg5OjE3ODIwNjQ3ODlfVjM)







