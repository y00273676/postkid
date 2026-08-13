package tui

import "go.planetmeican.com/yangguang/postkid/internal/model"

// ResponseMsg 携带一次 HTTP 请求的响应。
type ResponseMsg struct{ Resp model.Response }

// ErrorMsg 携带需要在状态栏显示的错误。
type ErrorMsg struct{ Err error }

// InfoMsg 携带一条普通状态提示。
type InfoMsg struct{ Text string }

// sendingMsg 标记开始发送，用于显示 spinner/提示。
type sendingMsg struct{}

// savedMsg 标记保存完成，用于清除 dirty 标记。
type savedMsg struct{}
