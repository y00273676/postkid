// Package tui 实现 postkid 的 Bubble Tea 界面。
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// Model 是 postkid 的 root tea.Model。
type Model struct {
	app *app.App

	width, height int
	focus         focus
	// returnFocus remembers the panel that opened a transient command palette.
	// Keeping it separate from focus makes Esc/q behave like a real back action
	// instead of unexpectedly jumping to the collection list.
	returnFocus focus
	showHelp    bool

	list     list.Model
	viewport viewport.Model
	palette  textinput.Model
	spinner  spinner.Model

	colls   []*model.Collection
	curColl *model.Collection
	curReq  *model.Request
	tab     tab

	resp    *model.Response
	sending bool
	dirty   bool

	statusMsg string
	err       error

	historyEntries []model.HistoryEntry
	historyIdx     int // 当前选中的历史索引
	showHistory    bool

	searching   bool
	searchInput textinput.Model

	// modal holds transient forms opened from the request/list panels.  Keeping
	// modal state in the TUI (rather than changing the application API) lets us
	// validate and cancel edits without touching the persisted request first.
	modal *modalState

	// workspaceModal is an independent CRUD form for collections/environments.
	// It owns its complete target list, including empty collections that cannot
	// appear in the flattened request list.
	workspaceModal *workspaceModalState

	// curlImport is a fully modal multiline paste/preview/save flow. It is
	// separate from the request editor because Enter must insert newlines here.
	curlImport *curlImportModalState
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

	l := list.New(items, reqDelegate{}, 0, 0)
	l.Title = "Collections"
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetShowTitle(true)
	l.Styles.Title = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.Styles.PaginationStyle = mutedStyle
	l.Styles.StatusBar = mutedStyle

	vp := viewport.New(0, 0)
	vp.SetContent("")

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(colAccent)),
	)

	p := textinput.New()
	p.Prompt = ":"
	p.PromptStyle = promptStyle
	p.Placeholder = "send | import curl | import postman <path> | env <name> | history"

	envName := "none"
	if e := a.CurrentEnvironment(); e != nil {
		envName = e.Name
	}

	return Model{
		app:         a,
		focus:       FocusList,
		returnFocus: FocusList,
		list:        l,
		viewport:    vp,
		palette:     p,
		spinner:     sp,
		colls:       colls,
		tab:         TabParams,
		statusMsg:   "env: " + envName + "  — press ? for help",
	}
}

// Init 启动时无需特殊命令。
func (m Model) Init() tea.Cmd { return nil }

// rebuildList 从 app 重新加载 collection 并重建左侧列表项。
func (m *Model) rebuildList() {
	srcColls := m.app.Collections()
	colls := make([]*model.Collection, len(srcColls))
	var items []list.Item
	for i := range srcColls {
		c := srcColls[i]
		colls[i] = &c
		for ri := range colls[i].Requests {
			items = append(items, listItem{coll: colls[i], req: &colls[i].Requests[ri]})
		}
	}
	m.colls = colls
	m.list.SetItems(items)
}

// allItems 返回全部 collection/request 列表项。
func (m Model) allItems() []list.Item {
	var items []list.Item
	for _, c := range m.colls {
		for ri := range c.Requests {
			items = append(items, listItem{coll: c, req: &c.Requests[ri]})
		}
	}
	return items
}

// filterItems 返回标题包含 query 的列表项（大小写不敏感）。
func (m Model) filterItems(query string) []list.Item {
	q := strings.ToLower(query)
	var items []list.Item
	for _, c := range m.colls {
		for ri := range c.Requests {
			item := listItem{coll: c, req: &c.Requests[ri]}
			if strings.Contains(strings.ToLower(item.Title()), q) ||
				strings.Contains(strings.ToLower(item.Description()), q) {
				items = append(items, item)
			}
		}
	}
	return items
}
