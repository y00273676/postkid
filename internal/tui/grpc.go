package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

func (m Model) openGRPCCommand(edit, discover bool) tea.Cmd {
	return func() tea.Msg { return GRPCOpenMsg{Edit: edit, Discover: discover} }
}

func (m *Model) openGRPCModal(edit bool) tea.Cmd {
	if m.app == nil {
		m.err = fmt.Errorf("application unavailable")
		return nil
	}
	if len(m.colls) == 0 {
		m.err = fmt.Errorf("no collection available")
		return nil
	}
	if edit {
		if m.curReq == nil || !m.curReq.IsGRPC() {
			m.err = fmt.Errorf("select a gRPC request first")
			return nil
		}
		m.grpcModal = newGRPCModal(m.colls, m.curColl, m.curReq, true)
	} else {
		m.grpcModal = newGRPCModal(m.colls, m.curColl, nil, false)
	}
	m.grpcModal.resize(m.width)
	m.err = nil
	return nil
}

func (m Model) updateGRPCModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.grpcModal == nil {
		return m, nil
	}
	if msg.Type == tea.KeyEscape {
		m.grpcModal = nil
		return m, nil
	}
	if msg.Type == tea.KeyCtrlG {
		m.grpcModal.discovering = true
		m.grpcModal.err = ""
		m.grpcDiscoverySeq++
		return m, m.discoverGRPC(m.grpcDiscoverySeq)
	}
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlS {
		cmd, err := m.commitGRPCModal()
		if err != nil {
			m.grpcModal.err = err.Error()
			return m, nil
		}
		m.grpcModal = nil
		return m, cmd
	}
	return m, m.grpcModal.update(msg)
}

func (m *Model) resolveGRPCCurrent() (model.ResolvedGRPCRequest, error) {
	if m.curReq == nil {
		return model.ResolvedGRPCRequest{}, fmt.Errorf("no request selected")
	}
	if m.app == nil {
		return model.ResolvedGRPCRequest{}, fmt.Errorf("application unavailable")
	}
	var coll model.Collection
	if m.curColl != nil {
		coll = *m.curColl
	}
	return m.app.ResolveGRPCRequest(*m.curReq, coll)
}

func (m Model) sendGRPCCurrent() tea.Cmd {
	if m.curReq == nil || !m.curReq.IsGRPC() {
		return m.errorCmd(fmt.Errorf("no gRPC request selected"))
	}
	resolved, err := m.resolveGRPCCurrent()
	if err != nil {
		return m.errorCmd(err)
	}
	appModel := m.app
	return tea.Sequence(
		func() tea.Msg { return sendingMsg{} },
		func() tea.Msg {
			return GRPCResponseMsg{Resp: appModel.SendGRPC(resolved), Resolved: resolved}
		},
	)
}

func (m Model) discoverGRPC(token uint64) tea.Cmd {
	if m.grpcModal == nil {
		return m.errorCmd(fmt.Errorf("gRPC form is not open"))
	}
	if m.app == nil {
		return m.errorCmd(fmt.Errorf("application unavailable"))
	}
	// Resolve through the application layer before discovery. Besides
	// environment substitution, this makes descriptor paths relative to the
	// collection YAML and lets the engine choose local descriptors without
	// dialing the target.
	var coll model.Collection
	if m.grpcModal.edit {
		if m.curReq == nil || m.curColl == nil {
			return m.errorCmd(fmt.Errorf("select a gRPC request first"))
		}
		coll = *m.curColl
	} else if m.grpcModal.collection >= 0 && m.grpcModal.collection < len(m.colls) {
		coll = *m.colls[m.grpcModal.collection]
	}
	var existing *model.Request
	if m.grpcModal.edit {
		existing = m.curReq
	}
	request := m.grpcModal.discoveryRequest(existing)
	resolved, err := m.app.ResolveGRPCDiscoveryRequest(request, coll)
	if err != nil {
		return m.errorCmd(err)
	}
	appModel := m.app
	return func() tea.Msg {
		services, err := appModel.DiscoverGRPCRequest(context.Background(), resolved)
		return GRPCDiscoveredMsg{Services: services, Err: err, Token: token}
	}
}

func (m *Model) commitGRPCModal() (tea.Cmd, error) {
	if m.grpcModal == nil {
		return nil, fmt.Errorf("gRPC form is not open")
	}
	var existing *model.Request
	if m.grpcModal.edit {
		existing = m.curReq
		if existing == nil || m.curColl == nil {
			return nil, fmt.Errorf("select a gRPC request first")
		}
	}
	req, err := m.grpcModal.request(existing)
	if err != nil {
		return nil, err
	}
	if m.grpcModal.edit {
		// Keep editing local until the normal Ctrl+S path writes the collection.
		// The nested gRPC metadata is canonical; Headers is left empty so the
		// existing HTTP header editor cannot accidentally shadow it.
		*m.curReq = req
		m.dirty = true
		m.statusMsg = "gRPC edited — Ctrl+S to save"
		return nil, nil
	}
	if m.grpcModal.collection < 0 || m.grpcModal.collection >= len(m.colls) {
		return nil, fmt.Errorf("invalid collection selection")
	}
	return m.addNewRequestCmd(m.colls[m.grpcModal.collection], req), nil
}
