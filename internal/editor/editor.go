// Package editor 用 $EDITOR 暂停 TUI、编辑请求 body。
package editor

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// DoneMsg 在 $EDITOR 退出后发送，携带编辑后的 body。
type DoneMsg struct {
	Body string
	Err  error
}

// Open 用 $EDITOR 打开临时文件编辑 body，返回 tea.Cmd。
// bubbletea 的 tea.ExecProcess 会自动 suspend/恢复终端。退出后读回文件内容并发出 DoneMsg。
func Open(body string) tea.Cmd {
	tmp, err := os.CreateTemp("", "postkid-body-*.json")
	if err != nil {
		return func() tea.Msg { return DoneMsg{Err: err} }
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(path)
		return func() tea.Msg { return DoneMsg{Err: err} }
	}
	tmp.Close()

	editorCmd := os.Getenv("EDITOR")
	if editorCmd == "" {
		editorCmd = "vi"
	}
	cmd := exec.Command(editorCmd, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		data, readErr := os.ReadFile(path)
		os.Remove(path)
		if readErr != nil {
			return DoneMsg{Err: readErr}
		}
		if execErr != nil {
			return DoneMsg{Err: execErr}
		}
		return DoneMsg{Body: string(data)}
	})
}
