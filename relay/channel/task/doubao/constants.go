package doubao

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
	"doubao-seedance-2-0-mini-260615",
	"dreamina-seedance-2-0-260128",
	"dreamina-seedance-2-0-fast-260128",
	"dreamina-seedance-2-0-mini-260615",
	"doubao-seedance-2-5-260628",
	"dreamina-seedance-2-5-260628",
}

var ChannelName = "doubao-video"

// Seedance 2.0 的视频输入/分辨率折扣已统一到共享包 relay/channel/task/seedance
// (精确矩阵 + 单一 video_pricing 倍率),不再单独维护 doubao-* 的折扣表。
