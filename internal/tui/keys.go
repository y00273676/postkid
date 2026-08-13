package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap 集中定义全部键位。
type keyMap struct {
	Quit     key.Binding
	Help     key.Binding
	Send     key.Binding
	Save     key.Binding
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	NextTab  key.Binding
	PrevTab  key.Binding
	EditBody key.Binding
	Command  key.Binding
	Back     key.Binding
}

// enterKey 用于在列表中打开选中项。
var enterKey = key.NewBinding(key.WithKeys("enter"))

var keys = keyMap{
	Quit:     key.NewBinding(key.WithKeys("q")),
	Help:     key.NewBinding(key.WithKeys("?")),
	Send:     key.NewBinding(key.WithKeys("s", "ctrl+r")),
	Save:     key.NewBinding(key.WithKeys("ctrl+s")),
	Up:       key.NewBinding(key.WithKeys("k", "up")),
	Down:     key.NewBinding(key.WithKeys("j", "down")),
	Left:     key.NewBinding(key.WithKeys("h", "left")),
	Right:    key.NewBinding(key.WithKeys("l", "right")),
	NextTab:  key.NewBinding(key.WithKeys("tab")),
	PrevTab:  key.NewBinding(key.WithKeys("shift+tab")),
	EditBody: key.NewBinding(key.WithKeys("e")),
	Command:  key.NewBinding(key.WithKeys(":")),
	Back:     key.NewBinding(key.WithKeys("esc")),
}

// ShortHelp 返回状态栏展示的键位提示。
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Save, k.Command, k.Help, k.Quit}
}

// FullHelp 返回 help overlay 展示的完整键位。
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Send, k.Save, k.EditBody},
		{k.NextTab, k.PrevTab, k.Command, k.Back},
		{k.Help, k.Quit},
	}
}
