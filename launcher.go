package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1)

	itemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedStyle = lipgloss.NewStyle().
			PaddingLeft(0).
			Foreground(lipgloss.Color("#EE6FF8")).
			Bold(true).
			SetString("> ")

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(1)
)

// --- Model ---

type item struct {
	title string
	path  string
	desc  string
}

type model struct {
	items    []item
	cursor   int
	status   string
	quitting bool
}

func initialModel() model {
	return model{
		items: []item{
			{title: "Start Capture UI (Old)", path: "capture_ui_old/capture_ui_old.exe", desc: "Legacy Windows Interface (Default)"},
			{title: "Start Capture UI (Fyne)", path: "capture_ui/capture_ui.exe", desc: "Modern UI (Fyne)"},
			{title: "Start Capture Auto", path: "capture_auto/capture_auto.exe", desc: "Background Auto Capture"},
			{title: "Start Server", path: "server/server.exe", desc: "Backend API & Processing"},
			{title: "Start AI Data Prepare", path: "ai_data_prepare/ai_data_prepare.exe", desc: "Preparing AI Data"},
			{title: "Exit", path: "", desc: "Close Launcher"},
		},
		cursor: 0,
		status: "Ready.",
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case statusMsg:
		m.status = string(msg)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case "enter", " ":
			selected := m.items[m.cursor]
			if selected.title == "Exit" {
				m.quitting = true
				return m, tea.Quit
			}

			return m, launchApp(selected)
		}
	}

	return m, nil
}

func launchApp(i item) tea.Cmd {
	return func() tea.Msg {
		// Use 'cmd /c start' to detach and spawn in new window/process
		cmd := exec.Command("cmd", "/c", "start", "", i.path)
		err := cmd.Start()

		status := fmt.Sprintf("Launched %s!", i.title)
		if err != nil {
			status = fmt.Sprintf("Error launching %s: %v", i.title, err)
		}

		// Return a custom message to update status (not implemented simply here, just print/log)
		// For simplicity in this structure, we won't loop the status back to model nicely without a type.
		// But we want to see it.
		// We really should return a statusMsg
		return statusMsg(status)
	}
}

type statusMsg string

func (m model) View() string {
	if m.quitting {
		return "Bye!\n"
	}

	s := titleStyle.Render("VN DATA PREPARE LAUNCHER") + "\n"

	for i, it := range m.items {
		cursor := "  "
		line := itemStyle.Render(it.title)

		if m.cursor == i {
			cursor = selectedStyle.String()
			line = selectedStyle.Render(it.title) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#555")).Render(it.desc)
		} else {
			line = itemStyle.Render(it.title)
		}

		s += fmt.Sprintf("%s%s\n", cursor, line)
	}

	s += statusStyle.Render(m.status)
	s += "\n\nPress q to quit.\n"

	return s
}

func main() {
	// Enable full color support on Windows
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// Update loop needs to handle statusMsg
func (m model) UpdateWrapper(msg tea.Msg) (tea.Model, tea.Cmd) {
	// We already have Update, let's fix the method receiver above to handle statusMsg
	// GO doesn't allow redefinition. I will paste the fixed Update in the actual output.
	return m, nil
}
