package dto

// AutoCreateUserPreviewResponse 是 GET /api/user/auto/preview 的响应数据部分。
// 字段语义请见 service.PreviewAutoCreateUser 以及
// docs/superpowers/specs/2026-06-18-auto-create-user-design.md。
type AutoCreateUserPreviewResponse struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Group    string `json:"group"`
	Quota    int    `json:"quota"`
}
