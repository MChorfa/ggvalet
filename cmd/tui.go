// cmd/tui.go — interactive terminal UI for GitLab Valet.
//
// Layout (80×24 example):
//
//	┌─ GitLab Valet ────────────── host: gitlab.thalesdigital.io ─┐
//	│  [Issues]  Epics  Milestones  Journal                       │
//	├───────────────────────────┬──────────────────────────────────┤
//	│  ● #42 Implement OIDC…   │  #42 Implement OIDC refresh      │
//	│  ● #41 Fix JWT expiry…   │                                  │
//	│  ✓ #40 SHIELD forensics  │  State:    ● opened              │
//	│                           │  Assignee: @nchorfa              │
//	│                           │  Milestone: Sprint 14            │
//	│                           │  Labels:   security · backend    │
//	│                           │  ──────────────────────          │
//	│                           │  Description…                    │
//	├───────────────────────────┴──────────────────────────────────┤
//	│  Tab/1-4: switch  j/k: navigate  /: filter  o: open  q: quit│
//	└──────────────────────────────────────────────────────────────┘
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ckodex/gitlabvalet/internal/journal"
	"github.com/spf13/cobra"
	gl "github.com/xanzy/go-gitlab"
)

// ─── Colour palette (CKODEX-DS-1) ────────────────────────────────────────────

var (
	tuiPrimary = lipgloss.Color("#38a3a5")
	tuiAccent  = lipgloss.Color("#bc96e6")
	tuiDim     = lipgloss.Color("#555555")
	tuiText    = lipgloss.Color("#dddddd")
	tuiBg      = lipgloss.Color("#00152b")
	tuiGreen   = lipgloss.Color("#22c55e")
	tuiOrange  = lipgloss.Color("#f97316")
	tuiYellow  = lipgloss.Color("#fbbf24")
)

var (
	tuiHeaderStyle = lipgloss.NewStyle().Background(tuiBg).Foreground(tuiPrimary).Bold(true).PaddingLeft(1)
	tuiTabOn       = lipgloss.NewStyle().Background(tuiPrimary).Foreground(lipgloss.Color("#000000")).Bold(true).PaddingLeft(2).PaddingRight(2)
	tuiTabOff      = lipgloss.NewStyle().Foreground(tuiDim).PaddingLeft(2).PaddingRight(2)
	tuiFooterStyle = lipgloss.NewStyle().Background(tuiBg).Foreground(tuiDim).PaddingLeft(1)
	tuiDivider     = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(tuiDim)
	tuiDetailTitle = lipgloss.NewStyle().Foreground(tuiAccent).Bold(true)
	tuiKey         = lipgloss.NewStyle().Foreground(tuiDim).Width(12)
	tuiVal         = lipgloss.NewStyle().Foreground(tuiText)
)

// ─── Tab ─────────────────────────────────────────────────────────────────────

type tuiTab int

const (
	tIssues tuiTab = iota
	tEpics
	tMilestones
	tJournal
	tCount
)

var tuiTabNames = []string{"Issues", "Epics", "Milestones", "Journal"}

// ─── List items ──────────────────────────────────────────────────────────────

type issItem struct{ v *gl.Issue }

func (i issItem) Title() string {
	if i.v.State == "closed" {
		return fmt.Sprintf("✓ #%d  %s", i.v.IID, i.v.Title)
	}
	return fmt.Sprintf("● #%d  %s", i.v.IID, i.v.Title)
}
func (i issItem) Description() string {
	var parts []string
	if i.v.Assignee != nil {
		parts = append(parts, "@"+i.v.Assignee.Username)
	}
	if i.v.Milestone != nil {
		parts = append(parts, "⬡ "+i.v.Milestone.Title)
	}
	if len(i.v.Labels) > 0 {
		parts = append(parts, strings.Join(i.v.Labels, " · "))
	}
	if len(parts) == 0 {
		return "  "
	}
	return strings.Join(parts, "  ")
}
func (i issItem) FilterValue() string {
	return fmt.Sprintf("%d %s %s", i.v.IID, i.v.Title, strings.Join(i.v.Labels, " "))
}

type epItem struct{ v *gl.Epic }

func (e epItem) Title() string {
	if e.v.State == "closed" {
		return fmt.Sprintf("◇ &%d  %s", e.v.IID, e.v.Title)
	}
	return fmt.Sprintf("◆ &%d  %s", e.v.IID, e.v.Title)
}
func (e epItem) Description() string {
	return fmt.Sprintf("@%s", e.v.Author.Username)
}
func (e epItem) FilterValue() string { return e.v.Title }

type msItem struct{ v *gl.Milestone }

func (m msItem) Title() string {
	due := ""
	if m.v.DueDate != nil {
		due = "  due:" + m.v.DueDate.String()
	}
	return fmt.Sprintf("⬡ %s%s", m.v.Title, due)
}
func (m msItem) Description() string {
	return fmt.Sprintf("(stats unavailable)")
}
func (m msItem) FilterValue() string { return m.v.Title }

type jItem struct {
	ts, op, entity, title, project, host string
	outcome                              string
}

func (j jItem) Title() string {
	icon := "✓"
	if j.outcome == "err" {
		icon = "✗"
	}
	return fmt.Sprintf("%s [%s %s]  %s", icon, j.entity, j.op, j.title)
}
func (j jItem) Description() string {
	return fmt.Sprintf("%s  %s  %s", j.ts, shortHostname(j.host), j.project)
}
func (j jItem) FilterValue() string { return j.title + " " + j.entity + " " + j.op }

// ─── Messages ─────────────────────────────────────────────────────────────────

type (
	tuiIssuesMsg     []*gl.Issue
	tuiEpicsMsg      []*gl.Epic
	tuiMilestonesMsg []*gl.Milestone
	tuiJournalMsg    []jItem
	tuiErrMsg        struct {
		tab tuiTab
		err error
	}
)

// ─── Model ───────────────────────────────────────────────────────────────────

type tuiModel struct {
	tab     tuiTab
	list    list.Model
	detail  viewport.Model
	width   int
	height  int
	ready   bool
	loading bool
	err     error
	host    string
	project string
	group   string

	loaded     [tCount]bool
	issues     []*gl.Issue
	epics      []*gl.Epic
	milestones []*gl.Milestone
}

func newTUI(host, project, group string) tuiModel {
	del := list.NewDefaultDelegate()
	del.Styles.SelectedTitle = del.Styles.SelectedTitle.
		Foreground(tuiPrimary).BorderLeftForeground(tuiPrimary)
	del.Styles.SelectedDesc = del.Styles.SelectedDesc.Foreground(tuiAccent)

	l := list.New(nil, del, 0, 0)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Title = ""
	l.Styles.Title = lipgloss.NewStyle()

	return tuiModel{
		list:    l,
		detail:  viewport.New(0, 0),
		host:    host,
		project: project,
		group:   group,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return m.loadCmd(tIssues)
}

// ─── Load commands ────────────────────────────────────────────────────────────

func (m tuiModel) loadCmd(t tuiTab) tea.Cmd {
	switch t {
	case tIssues:
		return func() tea.Msg {
			proj := m.project
			if proj == "" {
				proj = cfg.DefaultProject
			}
			var issues []*gl.Issue
			var err error
			if proj != "" {
				issues, _, err = glClient.GL.Issues.ListProjectIssues(proj,
					&gl.ListProjectIssuesOptions{
						State:       gl.Ptr("opened"),
						ListOptions: gl.ListOptions{PerPage: 50},
					})
			} else {
				issues, _, err = glClient.GL.Issues.ListIssues(&gl.ListIssuesOptions{
					Scope:       gl.Ptr("assigned_to_me"),
					State:       gl.Ptr("opened"),
					ListOptions: gl.ListOptions{PerPage: 50},
				})
			}
			if err != nil {
				return tuiErrMsg{t, err}
			}
			return tuiIssuesMsg(issues)
		}

	case tEpics:
		return func() tea.Msg {
			g := m.group
			if g == "" {
				g = cfg.DefaultGroup
			}
			if g == "" {
				return tuiEpicsMsg(nil)
			}
			epics, _, err := glClient.GL.Epics.ListGroupEpics(g,
				&gl.ListGroupEpicsOptions{
					State:       gl.Ptr("opened"),
					ListOptions: gl.ListOptions{PerPage: 50},
				})
			if err != nil {
				return tuiErrMsg{t, err}
			}
			return tuiEpicsMsg(epics)
		}

	case tMilestones:
		return func() tea.Msg {
			proj := m.project
			if proj == "" {
				proj = cfg.DefaultProject
			}
			if proj == "" {
				return tuiMilestonesMsg(nil)
			}
			ms, _, err := glClient.GL.Milestones.ListMilestones(proj,
				&gl.ListMilestonesOptions{
					State:       gl.Ptr("active"),
					ListOptions: gl.ListOptions{PerPage: 50},
				})
			if err != nil {
				return tuiErrMsg{t, err}
			}
			return tuiMilestonesMsg(ms)
		}

	case tJournal:
		return func() tea.Msg {
			since := time.Now().UTC().Add(-7 * 24 * time.Hour)
			f := journal.Filter{Since: since}
			if m.host != "" {
				f.Host = m.host
			}
			entries, err := glClient.Journal.Query(f)
			if err != nil {
				return tuiErrMsg{t, err}
			}
			items := make([]jItem, 0, len(entries))
			for _, e := range entries {
				items = append(items, jItem{
					ts:      e.Timestamp.Local().Format("01-02 15:04"),
					op:      string(e.Op),
					entity:  string(e.Entity),
					title:   e.Title,
					project: e.Project,
					host:    e.Host,
					outcome: string(e.Outcome),
				})
			}
			return tuiJournalMsg(items)
		}
	}
	return nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeComponents()
		if !m.ready {
			m.ready = true
		}

	case tuiIssuesMsg:
		m.issues = []*gl.Issue(msg)
		m.loaded[tIssues] = true
		m.loading = false
		items := make([]list.Item, len(m.issues))
		for i, v := range m.issues {
			items[i] = issItem{v}
		}
		m.list.SetItems(items)
		m.refreshDetail()

	case tuiEpicsMsg:
		m.epics = []*gl.Epic(msg)
		m.loaded[tEpics] = true
		m.loading = false
		items := make([]list.Item, len(m.epics))
		for i, v := range m.epics {
			items[i] = epItem{v}
		}
		m.list.SetItems(items)
		m.refreshDetail()

	case tuiMilestonesMsg:
		m.milestones = []*gl.Milestone(msg)
		m.loaded[tMilestones] = true
		m.loading = false
		items := make([]list.Item, len(m.milestones))
		for i, v := range m.milestones {
			items[i] = msItem{v}
		}
		m.list.SetItems(items)
		m.refreshDetail()

	case tuiJournalMsg:
		m.loaded[tJournal] = true
		m.loading = false
		items := make([]list.Item, len(msg))
		for i, ji := range msg {
			items[i] = ji
		}
		m.list.SetItems(items)
		m.refreshDetail()

	case tuiErrMsg:
		m.loading = false
		m.err = msg.err

	case tea.KeyMsg:
		// Let list handle filter input
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.tab = (m.tab + 1) % tCount
			return m, m.switchTab()
		case "shift+tab":
			m.tab = (m.tab + tCount - 1) % tCount
			return m, m.switchTab()
		case "1":
			m.tab = tIssues
			return m, m.switchTab()
		case "2":
			m.tab = tEpics
			return m, m.switchTab()
		case "3":
			m.tab = tMilestones
			return m, m.switchTab()
		case "4":
			m.tab = tJournal
			return m, m.switchTab()
		case "r":
			m.loaded[m.tab] = false
			m.loading = true
			return m, m.loadCmd(m.tab)
		case "o":
			if u := m.selectedURL(); u != "" {
				openURL(u)
			}
		case "j", "down", "k", "up":
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.refreshDetail()
			return m, cmd
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)
	m.detail, cmd = m.detail.Update(msg)
	cmds = append(cmds, cmd)
	m.refreshDetail()
	return m, tea.Batch(cmds...)
}

func (m *tuiModel) switchTab() tea.Cmd {
	m.list.ResetFilter()
	m.err = nil
	if !m.loaded[m.tab] {
		m.loading = true
		return m.loadCmd(m.tab)
	}
	m.refreshDetail()
	return nil
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	if !m.ready {
		return "\n  Loading GitLab Valet…"
	}
	header := m.viewHeader()
	footer := m.viewFooter()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 1 {
		bodyH = 1
	}

	listW := m.width * 42 / 100
	if listW < 18 {
		listW = 18
	}
	detailW := m.width - listW - 1

	m.list.SetSize(listW, bodyH)

	var leftContent string
	if m.loading {
		leftContent = lipgloss.NewStyle().Width(listW).Height(bodyH).Foreground(tuiDim).
			Render("  Loading…")
	} else if m.err != nil {
		leftContent = lipgloss.NewStyle().Width(listW).Height(bodyH).
			Foreground(lipgloss.Color("#ef4444")).
			Render(fmt.Sprintf("  Error:\n  %v", m.err))
	} else {
		leftContent = m.list.View()
	}

	left := tuiDivider.Width(listW).Height(bodyH).Render(leftContent)
	right := lipgloss.NewStyle().Width(detailW).Height(bodyH).Render(m.detail.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m tuiModel) viewHeader() string {
	var tabs []string
	for i, name := range tuiTabNames {
		if tuiTab(i) == m.tab {
			tabs = append(tabs, tuiTabOn.Render(name))
		} else {
			tabs = append(tabs, tuiTabOff.Render(name))
		}
	}
	tabRow := strings.Join(tabs, "")
	hostLabel := "  host: " + lipgloss.NewStyle().Foreground(tuiPrimary).Render(shortHostname(m.host))
	title := tuiHeaderStyle.Width(m.width).Render(
		lipgloss.NewStyle().Foreground(tuiAccent).Bold(true).Render("GitLab Valet") + hostLabel)
	tabLine := lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Width(m.width).Render(tabRow)
	return title + "\n" + tabLine
}

func (m tuiModel) viewFooter() string {
	return tuiFooterStyle.Width(m.width).Render(
		"  Tab/1-4: switch  ↑/k ↓/j: navigate  /: filter  o: open  r: reload  q: quit")
}

// ─── Detail pane ──────────────────────────────────────────────────────────────

func (m *tuiModel) refreshDetail() {
	idx := m.list.Index()
	var content string

	switch m.tab {
	case tIssues:
		if idx >= 0 && idx < len(m.issues) {
			content = tuiIssueDetail(m.issues[idx])
		}
	case tEpics:
		if idx >= 0 && idx < len(m.epics) {
			content = tuiEpicDetail(m.epics[idx])
		}
	case tMilestones:
		if idx >= 0 && idx < len(m.milestones) {
			content = tuiMSDetail(m.milestones[idx])
		}
	case tJournal:
		content = lipgloss.NewStyle().Foreground(tuiDim).
			Render("  Last 7 days of activity.\n  Press / to filter  o to open in browser.")
	}
	if content == "" {
		content = lipgloss.NewStyle().Foreground(tuiDim).Render("  (nothing selected)")
	}
	m.detail.SetContent(content)
}

func kv(k, v string) string {
	return tuiKey.Render(k+":") + " " + tuiVal.Render(v) + "\n"
}
func sep() string {
	return lipgloss.NewStyle().Foreground(tuiDim).Render(strings.Repeat("─", 38)) + "\n\n"
}

func tuiIssueDetail(iss *gl.Issue) string {
	var b strings.Builder
	b.WriteString(tuiDetailTitle.Render(fmt.Sprintf("#%d %s", iss.IID, iss.Title)) + "\n\n")
	stateStr := lipgloss.NewStyle().Foreground(tuiGreen).Render("● " + iss.State)
	if iss.State != "opened" {
		stateStr = lipgloss.NewStyle().Foreground(tuiDim).Render("✓ " + iss.State)
	}
	b.WriteString(kv("State", stateStr))
	if iss.Assignee != nil {
		b.WriteString(kv("Assignee", "@"+iss.Assignee.Username))
	}
	if iss.Milestone != nil {
		b.WriteString(kv("Milestone", iss.Milestone.Title))
	}
	if len(iss.Labels) > 0 {
		b.WriteString(kv("Labels", strings.Join(iss.Labels, " · ")))
	}
	b.WriteString(kv("Created", iss.CreatedAt.Format("2006-01-02 15:04")))
	b.WriteString(kv("URL", iss.WebURL))
	if iss.Description != "" {
		b.WriteString("\n" + sep())
		desc := iss.Description
		if len(desc) > 700 {
			desc = desc[:700] + "\n…"
		}
		b.WriteString(lipgloss.NewStyle().Foreground(tuiText).Render(desc))
	}
	return b.String()
}

func tuiEpicDetail(ep *gl.Epic) string {
	var b strings.Builder
	b.WriteString(tuiDetailTitle.Render(fmt.Sprintf("&%d %s", ep.IID, ep.Title)) + "\n\n")
	b.WriteString(kv("State", ep.State))
	b.WriteString(kv("Author", "@"+ep.Author.Username))
	if ep.StartDate != nil {
		b.WriteString(kv("Start", ep.StartDate.String()))
	}
	if ep.DueDate != nil {
		b.WriteString(kv("Due", ep.DueDate.String()))
	}
	b.WriteString(kv("Progress", "(stats unavailable)"))
	b.WriteString(kv("URL", ep.WebURL))
	return b.String()
}

func tuiMSDetail(ms *gl.Milestone) string {
	var b strings.Builder
	b.WriteString(tuiDetailTitle.Render(ms.Title) + "\n\n")
	b.WriteString(kv("State", ms.State))
	if ms.StartDate != nil {
		b.WriteString(kv("Start", ms.StartDate.String()))
	}
	if ms.DueDate != nil {
		b.WriteString(kv("Due", ms.DueDate.String()))
	}
	b.WriteString(kv("Statistics", "(unavailable)"))
	b.WriteString(kv("URL", ms.WebURL))
	if ms.Description != "" {
		b.WriteString("\n" + sep())
		b.WriteString(lipgloss.NewStyle().Foreground(tuiText).Render(ms.Description))
	}
	return b.String()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (m *tuiModel) resizeComponents() {
	listW := m.width * 42 / 100
	if listW < 18 {
		listW = 18
	}
	detailW := m.width - listW - 1
	headerH, footerH := 2, 1
	bodyH := m.height - headerH - footerH
	m.list.SetSize(listW, bodyH)
	m.detail.Width = detailW
	m.detail.Height = bodyH
}

func (m tuiModel) selectedURL() string {
	idx := m.list.Index()
	switch m.tab {
	case tIssues:
		if idx >= 0 && idx < len(m.issues) {
			return m.issues[idx].WebURL
		}
	case tEpics:
		if idx >= 0 && idx < len(m.epics) {
			return m.epics[idx].WebURL
		}
	case tMilestones:
		if idx >= 0 && idx < len(m.milestones) {
			return m.milestones[idx].WebURL
		}
	}
	return ""
}

func tuiProgressBar(pct, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	color := tuiGreen
	if pct < 40 {
		color = tuiOrange
	} else if pct < 75 {
		color = tuiYellow
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", url, err)
	}
}

// ─── Command ──────────────────────────────────────────────────────────────────

func tuiCmd() *cobra.Command {
	var project, group string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI — browse issues, epics, milestones, journal",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.DefaultProject
			}
			if group == "" {
				group = cfg.DefaultGroup
			}
			m := newTUI(cfg.Host, project, group)
			p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err := p.Run()
			return err
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project to browse (default from env)")
	cmd.Flags().StringVarP(&group, "group", "g", "", "Group for epics (default from env)")
	return cmd
}
