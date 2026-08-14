package tui

import "go.planetmeican.com/yangguang/postkid/internal/model"

// ResponseMsg 携带一次 HTTP 请求的响应。
type ResponseMsg struct {
	Resp     model.Response
	Resolved model.ResolvedRequest
}

// ErrorMsg 携带需要在状态栏显示的错误。
type ErrorMsg struct{ Err error }

// InfoMsg 携带一条普通状态提示。
type InfoMsg struct{ Text string }

// sendingMsg 标记开始发送，用于显示 spinner/提示。
type sendingMsg struct{}

// savedMsg 标记保存完成，用于清除 dirty 标记。
type savedMsg struct{}

// HistoryMsg 携带历史记录列表。
type HistoryMsg struct {
	Entries []model.HistoryEntry
}

// ListUpdatedMsg 通知 collection 数据变更，需要重建左侧列表。
type ListUpdatedMsg struct{}

// SearchModeMsg 进入/退出搜索模式。
type SearchModeMsg struct {
	Active bool
}

// NewRequestMsg opens the request creation form. It is emitted by the
// command palette because commands execute as tea.Cmd values, while the form
// itself is owned by the model and must be created synchronously in Update.
type NewRequestMsg struct{}

// WorkspaceModalMsg opens a collection/environment CRUD form. Target is
// optional for rename/delete; when omitted, the form lets the user choose from
// the full application snapshot.
type WorkspaceModalMsg struct {
	Resource string // collection or environment
	Action   string // new, rename, delete
	Target   string
}

// WorkspaceUpdatedMsg tells the TUI to refresh its collection snapshot after a
// successful workspace operation and to show a concise status message.
type WorkspaceUpdatedMsg struct {
	Resource string
	Action   string
	Name     string
}

// CurlImportOpenMsg opens the multiline cURL import flow.
type CurlImportOpenMsg struct{}

// CurlImportSavedMsg reports a successfully persisted imported request.
type CurlImportSavedMsg struct {
	Collection string
	Name       string
}

// CurlImportSaveFailedMsg keeps the import target form open after persistence
// fails (for example, when the chosen request name already exists).
type CurlImportSaveFailedMsg struct{ Err error }

// PostmanImportSavedMsg reports a successfully imported and persisted
// Postman collection. Imported is the number of requests in the persisted
// collection and is used for the concise status-bar summary.
type PostmanImportSavedMsg struct {
	Collection string
	Imported   int
	Path       string
}

// PostmanImportSaveFailedMsg reports a read, parse, or persistence failure.
// The current list selection remains untouched when this message is handled.
type PostmanImportSaveFailedMsg struct {
	Path string
	Err  error
}

// PostmanEnvironmentImportSavedMsg reports a successfully imported and
// selected Postman environment.
type PostmanEnvironmentImportSavedMsg struct {
	Environment string
	Imported    int
	Path        string
}

// PostmanEnvironmentImportSaveFailedMsg reports a read, parse, or persistence
// failure. The current environment selection remains untouched.
type PostmanEnvironmentImportSaveFailedMsg struct {
	Path string
	Err  error
}

// GRPCOpenMsg opens the protocol-specific request form. Discover asks the
// form to inspect its configured descriptor source immediately.
type GRPCOpenMsg struct {
	Edit     bool
	Discover bool
}

// GRPCDiscoveredMsg carries services and methods returned by local descriptors
// or server reflection.
type GRPCDiscoveredMsg struct {
	Services []model.GRPCService
	Err      error
	Token    uint64
}

// GRPCResponseMsg carries one unary gRPC response.
type GRPCResponseMsg struct {
	Resp     model.GRPCResponse
	Resolved model.ResolvedGRPCRequest
}
