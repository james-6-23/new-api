package dto

// CreateUserRequest 是 POST /api/user/ 的请求体。
//
// 历史上 CreateUser 直接把 body 反序列化进 model.User，导致 Group/Quota 这种
// 应该可选的字段无法表达"想保留默认"和"显式置零"的语义差异。AutoCreateUser
// 功能需要后者，所以这里改用单独的 DTO，并按 CLAUDE.md Rule 6 把所有可选标量
// 字段升级为指针：
//
//   - 客户端漏传 → 反序列化得到 nil → 服务端不去写对应字段（GORM 列默认值生效）。
//   - 客户端显式发 0 / false → 反序列化得到 *T(0) → 服务端按显式值写入。
//
// 非指针的 string 字段（Username/Password/DisplayName/Remark）维持非指针，
// 因为对它们而言"空字符串"和"未传"在业务上含义相同。
type CreateUserRequest struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName string  `json:"display_name"`
	Role        int     `json:"role"`
	Group       *string `json:"group,omitempty"`
	Quota       *int    `json:"quota,omitempty"`
	Remark      string  `json:"remark,omitempty"`
}
