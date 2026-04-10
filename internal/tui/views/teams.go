/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package views

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// teamActionMode tracks the current interaction state.
type teamActionMode int

const (
	teamModeNormal  teamActionMode = iota
	teamModeInput                  // text input active
	teamModeConfirm                // y/n confirmation
	teamModeResult                 // success/error message displayed
)

// addMember action steps: first collect email, then role.
const (
	teamStepEmail = 0
	teamStepRole  = 1
)

// createTeam action steps: first collect name, then display name.
const (
	teamCreateStepName        = 0
	teamCreateStepDisplayName = 1
)

// pendingTeamAction identifies which action is being executed.
const (
	teamActionNone      = ""
	teamActionAddMember = "add-member"
	teamActionCreate    = "create"
	teamActionDelete    = "delete"
)

type teamListMsg struct {
	rows [][]string
	err  error
}

// teamActionResultMsg carries the result of an async team action.
type teamActionResultMsg struct {
	err     error
	action  string
	deleted bool
}

// TeamListView displays Team resources.
type TeamListView struct {
	client  *client.Client
	Admin   bool
	table   components.Table
	loading bool
	err     error
	width   int
	height  int

	// Action state
	mode          teamActionMode
	pendingAction string
	input         textinput.Model
	confirmMsg    string
	resultMsg     string
	resultIsError bool

	// Add member multi-step state
	addMemberStep  int    // 0=email, 1=role
	addMemberEmail string // captured from step 0

	// Create team multi-step state
	createStep     int    // 0=name, 1=displayName
	createTeamName string // captured from step 0
}

// NewTeamListView creates the team list view.
func NewTeamListView(c *client.Client, admin bool) TeamListView {
	ti := textinput.New()
	ti.CharLimit = 128
	return TeamListView{
		client:  c,
		Admin:   admin,
		table:   components.NewTable([]string{"NAME", "DISPLAY NAME", "PHASE", "CLUSTERS", "MEMBERS", "NAMESPACE", "AGE"}),
		loading: true,
		input:   ti,
	}
}

// IsFiltering returns true if the table is in filter mode.
func (v *TeamListView) IsFiltering() bool {
	return v.table.Filtering()
}

// IsInActionMode returns true when a team action prompt is active.
func (v *TeamListView) IsInActionMode() bool {
	return v.mode != teamModeNormal
}

// Init fetches team data.
func (v TeamListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v TeamListView) Update(msg tea.Msg) (TeamListView, tea.Cmd) {
	switch msg := msg.(type) {
	case teamListMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.table.SetRows(msg.rows)
		}
		return v, nil

	case teamActionResultMsg:
		v.loading = false
		if msg.err != nil {
			v.mode = teamModeResult
			v.resultMsg = msg.err.Error()
			v.resultIsError = true
		} else {
			v.mode = teamModeResult
			v.resultMsg = v.teamActionSuccessMessage()
			v.resultIsError = false
		}
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		v.table.SetSize(msg.Width, msg.Height-4)
		return v, nil

	case tea.KeyMsg:
		switch v.mode {
		case teamModeInput:
			return v.updateInputMode(msg)
		case teamModeConfirm:
			return v.updateConfirmMode(msg)
		case teamModeResult:
			return v.updateResultMode(msg)
		default:
			return v.updateNormalMode(msg)
		}
	}
	return v, nil
}

// updateNormalMode handles keys when no action is active.
func (v TeamListView) updateNormalMode(msg tea.KeyMsg) (TeamListView, tea.Cmd) {
	if v.table.Filtering() {
		cmd := v.table.Update(msg)
		return v, cmd
	}
	switch msg.String() {
	case "r":
		v.loading = true
		return v, v.fetch()
	case "a":
		row := v.table.SelectedRow()
		if row != nil && len(row) > 0 {
			v.mode = teamModeInput
			v.pendingAction = teamActionAddMember
			v.addMemberStep = teamStepEmail
			v.addMemberEmail = ""
			v.input.Reset()
			v.input.Placeholder = "Email address: "
			v.input.Focus()
			return v, nil
		}
	case "c":
		if !v.Admin {
			return v, nil
		}
		v.mode = teamModeInput
		v.pendingAction = teamActionCreate
		v.createStep = teamCreateStepName
		v.createTeamName = ""
		v.input.Reset()
		v.input.Placeholder = "Team name: "
		v.input.Focus()
		return v, nil
	case "d":
		if !v.Admin {
			return v, nil
		}
		row := v.table.SelectedRow()
		if row != nil && len(row) > 0 {
			v.mode = teamModeConfirm
			v.pendingAction = teamActionDelete
			v.confirmMsg = fmt.Sprintf("Delete team %s? (y/n)", row[0])
			return v, nil
		}
	default:
		cmd := v.table.Update(msg)
		return v, cmd
	}
	return v, nil
}

// updateInputMode handles keys during text input.
func (v TeamListView) updateInputMode(msg tea.KeyMsg) (TeamListView, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.mode = teamModeNormal
		v.pendingAction = teamActionNone
		v.input.Blur()
		return v, nil
	case "enter":
		value := strings.TrimSpace(v.input.Value())
		if value == "" {
			v.mode = teamModeNormal
			v.pendingAction = teamActionNone
			v.input.Blur()
			return v, nil
		}

		switch v.pendingAction {
		case teamActionAddMember:
			return v.handleAddMemberInput(value)
		case teamActionCreate:
			return v.handleCreateInput(value)
		}

		return v, nil
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return v, cmd
	}
}

// handleAddMemberInput processes input for the add-member multi-step flow.
func (v TeamListView) handleAddMemberInput(value string) (TeamListView, tea.Cmd) {
	if v.addMemberStep == teamStepEmail {
		v.addMemberEmail = value
		v.addMemberStep = teamStepRole
		v.input.Reset()
		v.input.Placeholder = "Role (admin/operator/viewer): "
		v.input.Focus()
		return v, nil
	}

	// Step 1: role collected, execute the action
	role := strings.ToLower(value)
	if role != "admin" && role != "operator" && role != "viewer" {
		v.mode = teamModeResult
		v.resultMsg = fmt.Sprintf("Invalid role %q: must be admin, operator, or viewer", value)
		v.resultIsError = true
		v.input.Blur()
		return v, nil
	}

	v.input.Blur()
	v.loading = true

	row := v.table.SelectedRow()
	teamName := ""
	if row != nil && len(row) > 0 {
		teamName = row[0]
	}
	email := v.addMemberEmail

	return v, func() tea.Msg {
		return v.doAddMember(teamName, email, role)
	}
}

// handleCreateInput processes input for the create-team multi-step flow.
func (v TeamListView) handleCreateInput(value string) (TeamListView, tea.Cmd) {
	if v.createStep == teamCreateStepName {
		v.createTeamName = value
		v.createStep = teamCreateStepDisplayName
		v.input.Reset()
		v.input.Placeholder = "Display name: "
		v.input.Focus()
		return v, nil
	}

	// Step 1: display name collected, execute
	v.input.Blur()
	v.loading = true

	teamName := v.createTeamName
	displayName := value

	return v, func() tea.Msg {
		return v.doCreateTeam(teamName, displayName)
	}
}

// updateConfirmMode handles keys when a y/n prompt is active.
func (v TeamListView) updateConfirmMode(msg tea.KeyMsg) (TeamListView, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		v.loading = true
		row := v.table.SelectedRow()
		teamName := ""
		if row != nil && len(row) > 0 {
			teamName = row[0]
		}
		return v, func() tea.Msg {
			return v.doDeleteTeam(teamName)
		}
	case "n", "N", "esc":
		v.mode = teamModeNormal
		v.pendingAction = teamActionNone
		return v, nil
	}
	return v, nil
}

// updateResultMode handles keys when showing a result message.
func (v TeamListView) updateResultMode(msg tea.KeyMsg) (TeamListView, tea.Cmd) {
	v.mode = teamModeNormal
	v.pendingAction = teamActionNone
	v.resultMsg = ""
	v.loading = true
	return v, v.fetch()
}

// doAddMember patches the Team to add a user to spec.access.users.
func (v *TeamListView) doAddMember(teamName, email, role string) teamActionResultMsg {
	ctx := context.Background()

	// Get the current Team resource
	team, err := v.client.Dynamic.Resource(client.TeamGVR).Get(ctx, teamName, metav1.GetOptions{})
	if err != nil {
		return teamActionResultMsg{err: fmt.Errorf("getting team %s: %w", teamName, err), action: teamActionAddMember}
	}

	// Extract existing users
	users, _, _ := unstructured.NestedSlice(team.Object, "spec", "access", "users")

	// Check if user already exists
	for _, u := range users {
		userMap, ok := u.(map[string]interface{})
		if !ok {
			continue
		}
		if client.GetNestedString(userMap, "email") == email {
			return teamActionResultMsg{err: fmt.Errorf("user %s already exists in team %s", email, teamName), action: teamActionAddMember}
		}
	}

	// Append new user
	newUser := map[string]interface{}{
		"email": email,
		"role":  role,
	}
	users = append(users, newUser)

	// Build strategic merge patch
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"access": map[string]interface{}{
				"users": users,
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return teamActionResultMsg{err: fmt.Errorf("marshaling patch: %w", err), action: teamActionAddMember}
	}

	_, err = v.client.Dynamic.Resource(client.TeamGVR).Patch(
		ctx, teamName, types.MergePatchType, patchBytes, metav1.PatchOptions{},
	)
	if err != nil {
		return teamActionResultMsg{err: fmt.Errorf("adding member to team: %w", err), action: teamActionAddMember}
	}

	return teamActionResultMsg{action: teamActionAddMember}
}

// doCreateTeam creates a new Team CRD.
func (v *TeamListView) doCreateTeam(name, displayName string) teamActionResultMsg {
	ctx := context.Background()

	team := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "Team",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"displayName": displayName,
			},
		},
	}

	_, err := v.client.Dynamic.Resource(client.TeamGVR).Create(ctx, team, metav1.CreateOptions{})
	if err != nil {
		return teamActionResultMsg{err: fmt.Errorf("creating team: %w", err), action: teamActionCreate}
	}

	return teamActionResultMsg{action: teamActionCreate}
}

// doDeleteTeam deletes a Team CRD.
func (v *TeamListView) doDeleteTeam(name string) teamActionResultMsg {
	ctx := context.Background()

	err := v.client.Dynamic.Resource(client.TeamGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return teamActionResultMsg{err: fmt.Errorf("deleting team: %w", err), action: teamActionDelete}
	}

	return teamActionResultMsg{action: teamActionDelete, deleted: true}
}

// teamActionSuccessMessage returns a human-readable success message for the current action.
func (v *TeamListView) teamActionSuccessMessage() string {
	switch v.pendingAction {
	case teamActionAddMember:
		return "Member added. Press any key to refresh."
	case teamActionCreate:
		return "Team created. Press any key to refresh."
	case teamActionDelete:
		return "Team deleted. Press any key to refresh."
	}
	return "Action completed. Press any key to continue."
}

// KeyLegend returns the action keys available for the team view.
func (v *TeamListView) KeyLegend() string {
	dimStyle := styles.DimStyle
	keyStyle := styles.KeyLegendStyle

	legend := dimStyle.Render("  ") +
		keyStyle.Render("j/k") + dimStyle.Render(":navigate  ") +
		keyStyle.Render("a") + dimStyle.Render(":add member  ")

	if v.Admin {
		legend += keyStyle.Render("c") + dimStyle.Render(":create  ") +
			keyStyle.Render("d") + dimStyle.Render(":delete  ")
	}

	legend += keyStyle.Render("/") + dimStyle.Render(":filter  ") +
		keyStyle.Render("r") + dimStyle.Render(":refresh  ") +
		keyStyle.Render("?") + dimStyle.Render(":help  ") +
		keyStyle.Render("q") + dimStyle.Render(":quit")

	return legend
}

// View renders the team list.
func (v TeamListView) View() string {
	if v.loading {
		return "  Loading teams..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}

	var b strings.Builder
	b.WriteString(v.table.View())

	// Action prompt/result overlay
	if prompt := v.renderActionPrompt(); prompt != "" {
		b.WriteString("\n")
		b.WriteString(prompt)
	}

	return b.String()
}

// renderActionPrompt renders the inline prompt for the current action mode.
func (v TeamListView) renderActionPrompt() string {
	switch v.mode {
	case teamModeInput:
		return styles.ActionPromptStyle.Render(v.input.Placeholder) + v.input.View()
	case teamModeConfirm:
		return styles.ActionConfirmStyle.Render(v.confirmMsg)
	case teamModeResult:
		if v.resultIsError {
			return styles.ActionErrorStyle.Render("Error: " + v.resultMsg)
		}
		return styles.ActionSuccessStyle.Render(v.resultMsg)
	}
	return ""
}

func (v TeamListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.TeamGVR).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return teamListMsg{err: fmt.Errorf("listing Teams: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, tm := range list.Items {
			rows[i] = []string{
				tm.GetName(),
				client.GetNestedString(tm.Object, "spec", "displayName"),
				client.GetNestedString(tm.Object, "status", "phase"),
				fmt.Sprintf("%d", client.GetNestedInt64(tm.Object, "status", "clusterCount")),
				fmt.Sprintf("%d", client.GetNestedInt64(tm.Object, "status", "memberCount")),
				client.GetNestedString(tm.Object, "status", "namespace"),
				output.FormatAge(tm.GetCreationTimestamp().Time),
			}
		}
		return teamListMsg{rows: rows}
	}
}
