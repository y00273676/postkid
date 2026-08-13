// Package tui 实现 tpost 的 Bubble Tea 界面。
package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

type focus int

const (
	FocusList focus = iota
	FocusRequest
	FocusResponse

	FocusCommand
)

type tab int

const (
	TabParams tab = iota
	TabHeaders
	TabBody
	TabAuth
)

// Model 是 tpost 的 root tea.Model。
type Model struct {
	app *app.App

	width, height int
	focus         focus
	showHelp      bool

	list     list.Model
	viewport viewport.Model
	palette  textinput.Model

	colls  []*model.Collection
	curColl *model.Collection
	curReq  *model.Request
	tab     tab

	resp    *model.Response
	sending bool
	dirty   bool

	statusMsg string
	err       error
}

// listItem 适配 list.Item，把 collection/request 平铺进左侧列表。
type listItem struct {
	coll *model.Collection
	req  *model.Request
}

func (i listItem) FilterValue() string { return i.req.Name }
func (i listItem) Title() string       { return i.coll.Name + "/" + i.req.Name }
func (i listItem) Description() string { return i.req.Method + "  " + i.req.URL }

// New 构造初始 Model。
func New(a *app.App) Model {
	// 复制 collection 为独立指针，避免编辑时影响 app 内部 slice
	srcColls := a.Collections()
	colls := make([]*model.Collection, len(srcColls))
	var items []list.Item
	for i := range srcColls {
		c := srcColls[i]
		colls[i] = &c
		for ri := range colls[i].Requests {
			items = append(items, listItem{coll: colls[i], req: &colls[i].Requests[ri]})
		}
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Collections"
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(true)

	vp := viewport.New(0, 0)
	vp.SetContent("")

	p := textinput.New()
	p.Prompt = ":"
	p.PromptStyle = promptStyle

	envName := "none"
	if e := a.CurrentEnvironment(); e != nil {
		envName = e.Name
	}

	return Model{
		app:       a,
		focus:     FocusList,
		list:      l,
		viewport:  vp,
		palette:   p,
		colls:     colls,
		tab:       TabParams,
		statusMsg: "env: " + envName + "  — press ? for help",
	}
}

// Init 启动时无需特殊命令。
func (m Model) Init() tea.Cmd { return nil }
