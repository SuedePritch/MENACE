package tui

import (
	"fmt"
	"strings"

	"menace/internal/store"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProposalModal encapsulates the proposal review modal state.
type ProposalModal struct {
	proposal     store.Proposal
	proposalIdx  int
	proposalView viewport.Model
}

// NewProposalModal creates a proposal modal for the given proposal.
func NewProposalModal(proposals []store.Proposal, idx, width int) *ProposalModal {
	p := proposals[idx]
	pm := &ProposalModal{
		proposal:     p,
		proposalIdx:  idx,
		proposalView: viewport.New(width-4, 20),
	}

	wrapW := width - 6
	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("Description") + "\n")
	content.WriteString(strings.Join(wordWrap(p.Description, wrapW), "\n") + "\n\n")

	if p.Instruction != "" {
		content.WriteString(lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("Instructions") + "\n")
		content.WriteString(strings.Join(wordWrap(p.Instruction, wrapW), "\n") + "\n\n")
	}

	if len(p.Subtasks) > 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("Subtasks") + "\n")
		for i, s := range p.Subtasks {
			wrapped := strings.Join(wordWrap(s.Description, wrapW-5), "\n     ")
			content.WriteString(fmt.Sprintf("  %d. %s\n", i+1, wrapped))
		}
	}

	pm.proposalView.SetContent(content.String())
	pm.proposalView.GotoTop()
	return pm
}

func (pm *ProposalModal) WantsRawKeys() bool        { return false }
func (pm *ProposalModal) HandleRawKey(string) tea.Cmd { return nil }

// Resize updates viewport dimensions.
func (pm *ProposalModal) Resize(w, h int) {
	pm.proposalView.Width = w - 4
	pm.proposalView.Height = h - 5
}

// Update processes an action and returns a command for the parent.
func (pm *ProposalModal) Update(act action) tea.Cmd {
	switch act {
	case actEscape:
		return func() tea.Msg { return modalCloseMsg{} }
	case actDown:
		pm.proposalView.LineDown(1)
	case actUp:
		pm.proposalView.LineUp(1)
	case actHalfDown:
		pm.proposalView.HalfViewDown()
	case actHalfUp:
		pm.proposalView.HalfViewUp()
	case actTop:
		pm.proposalView.GotoTop()
	case actBottom:
		pm.proposalView.GotoBottom()
	case actApprove:
		return func() tea.Msg {
			return proposalApprovedMsg{Index: pm.proposalIdx, Proposal: pm.proposal}
		}
	case actCancel:
		return func() tea.Msg {
			return proposalRejectedMsg{Index: pm.proposalIdx, ProposalID: pm.proposal.ID}
		}
	}
	return nil
}

// View renders the proposal modal.
func (pm *ProposalModal) View(w, h int) string {
	contentH := h - 3
	contentW := w - 4

	pm.proposalView.Width = contentW
	pm.proposalView.Height = contentH

	box := bentoBox.BorderForeground(ColorAccent).Width(contentW).Height(contentH).
		Render(pm.proposalView.View())
	box = injectPanelTitle(box, "proposal", true)

	header := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Padding(0, 1).Render("◆ " + pm.proposal.Description)

	help := " " + renderHelp(
		helpPair(modalKeys, actDown, actUp, "scroll"),
		helpKey(modalKeys, actApprove),
		helpKeyLabel(modalKeys, actCancel, "reject"),
		helpKeyLabel(modalKeys, actEscape, "close"),
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, box, help)
}
