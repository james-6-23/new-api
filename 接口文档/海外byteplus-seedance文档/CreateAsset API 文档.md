&nbsp;

`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateAsset&Version=2024-01-01`

Create an Asset within the specified asset group. 

<div data-tips="true" data-tips-type="warning" data-tips-is-title="true">Warning</div>


<div data-tips="true" data-tips-type="warning">This API is asynchronous. Processing may be queued, which can increase ingestion time. Upload\-time SLAs are not guaranteed. </div>


<div data-tips="true" data-tips-type="warning">Higher latency is expected when uploading video assets.</div>


<div data-tips="true" data-tips-type="warning">After creation, poll GetAsset and use the Asset only after the status becomes <code>Active</code>. If the status becomes <code>Failed</code>, processing has failed.</div>


<div data-tips="true" data-tips-type="warning"></div>



<Tabs>
<Tab zoneid="mLwbCPYO" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="hHI9cNLl" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="PrUYA8XZ"></span>
## Request Parameters

<span id="6M6y7aYy"></span>
### Request Body


---



**GroupId** `string` <span data-api-tag="require|bEMmDE">必选</span>

The ID of the Asset Group that the Asset belongs to.


---



**URL** `string` <span data-api-tag="require|TeAZkx">必选</span>

A publicly accessible URL of the Asset.


---



**Name** `string`

The name of the Asset, up to 64 characters. 

**Note**: This field is used only for fuzzy search when calling the ** ** ListAssets API and is not included in model inference. For details on generating videos with assets, see [Generate videos using portrait assets ](https://docs.byteplus.com/en/docs/ModelArk/2333565?lang=en#generate-video-using-portrait-assets)and [FAQ 4](https://docs.byteplus.com/en/docs/ModelArk/2333565?lang=en#faqs).


---



**AssetType** `string` <span data-api-tag="require|4kELFX">必选</span>

The Asset type. Valid values:


* `Image`: Image

* `Video`: Video

* `Audio`: Audio


<div data-tips="true" data-tips-type="tip" data-tips-is-title="true">Note</div>


<div data-tips="true" data-tips-type="tip"><strong>For image/video/audio assets, only URL upload is supported. Base64 is not supported.</strong></div>


<div data-tips="true" data-tips-type="tip" data-tips-is-title="true"><strong>Requirements for a single image</strong></div>



* <div data-tips="true" data-tips-type="tip">Formats: jpeg, png, webp, bmp, tiff, gif, heic/heif</div>


* <div data-tips="true" data-tips-type="tip">Aspect ratio (W/H): (0.4, 2.5)</div>


* <div data-tips="true" data-tips-type="tip">Width/height (px): (300, 6000)</div>


* <div data-tips="true" data-tips-type="tip">Size: < 30 MB per image</div>



<div data-tips="true" data-tips-type="tip" data-tips-is-title="true"><strong>Requirements for a single video</strong></div>



* <div data-tips="true" data-tips-type="tip">Formats: mp4, mov</div>


* <div data-tips="true" data-tips-type="tip">Resolution: 480p, 720p, 1080p</div>


* <div data-tips="true" data-tips-type="tip">Duration: [2, 15] seconds</div>


* <div data-tips="true" data-tips-type="tip">Dimensions:</div>


   * <div data-tips="true" data-tips-type="tip">Aspect ratio (W/H): [0.4, 2.5]</div>


   * <div data-tips="true" data-tips-type="tip">Width/height (px): [300, 6000]</div>


   * <div data-tips="true" data-tips-type="tip">Total pixels (W×H): [409600, 2086876] (e.g., 640×640=409600, 834×1112=927408)</div>


* <div data-tips="true" data-tips-type="tip">Size: ≤ 50 MB per video</div>


* <div data-tips="true" data-tips-type="tip">FPS: [24, 60]</div>



<div data-tips="true" data-tips-type="tip" data-tips-is-title="true"><strong>Requirements for a single audio</strong></div>



* <div data-tips="true" data-tips-type="tip">Formats: wav, mp3</div>


* <div data-tips="true" data-tips-type="tip">Duration: [2, 15] seconds</div>


* <div data-tips="true" data-tips-type="tip">Size: ≤ 15 MB per audio</div>




---



**Moderation** `object`

Specifies whether to turn off the Content Pre\-filter review for the current asset. 


**Attribute**

**Strategy ** `string` <span data-api-tag="require|bEMmDE">必选</span>

Specifies the Content Pre\-filter review strategy for the current asset. 

Available values:


* `Default`: Content Pre\-filter review is on for the current asset.

* `Skip`: Skip most non\-baseline content security review policies.



---



**ProjectName** `string`

The name of the project to which the resource belongs. 

The default value is default. If the resource is not in the default project, you must enter the correct project name. For more information about project, see the related [IAM docs](https://docs.byteplus.com/en/docs/byteplus-platform/docs-managing-projects). 

**Note**: The **ProjectName **  must be consistent with the Asset Group to be created. 

<span id="wZDLhNZh"></span>
## Response Parameters


---



**Id** `string`

The ID of the asset. 


---



<span id="eKNScViV"></span>
## Request Example

```text
POST /?Action=CreateAsset&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "GroupId": "group-2026**********-*****",
  "URL": "https://example.com/image.jpg",
  "AssetType": "Image",
  "Moderation": {
      "Strategy": "Skip"
      }
}
```


<span id="2jgXYOYM"></span>
## Response Example

```json
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "CreateAsset",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Id": "Asset-2026**********-*****"
  }
}
```




