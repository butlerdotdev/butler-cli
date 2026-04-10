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

// userActionMode tracks the current interaction state.
type userActionMode int

const (
	userModeNormal  userActionMode = iota
	userModeInput                  // text input active
	userModeConfirm                // y/n confirmation
	userModeResult                 // success/error message displayed
)

// createUser action steps: first collect email, then display name.
const (
	userCreateStepEmail       = 0
	userCreateStepDisplayName = 1
)

// pendingUserAction identifies which action is being executed.
const (
	userActionNone    = ""
	userActionCreate  = "create"
	userActionDelete  = "delete"
	userActionToggle  = "toggle"
)

type userListMsg struct {
	rows [][]string
	err  error
}

// userActionResultMsg carries the result of an async user action.
type userActionResultMsg struct {
	err    error
	action string
}

// UserListView displays User resources.
type UserListView struct {
	client  *client.Client
	Admin   bool
	table   components.Table
	loading bool
	err     error
	width   int
	height  int

	// Action state
	mode          userActionMode
	pendingAction string
	input         textinput.Model
	confirmMsg    string
	resultMsg     string
	resultIsError bool

	// Create user multi-step state
	createStep      int
	createUserEmail string
}

// NewUserListView creates the user list view.
func NewUserListView(c *client.Client, admin bool) UserListView {
	ti := textinput.New()
	ti.CharLimit = 128
	return UserListView{
		client:  c,
		Admin:   admin,
		table:   components.NewTable([]string{"NAME", "EMAIL", "PHASE", "AUTH TYPE", "ADMIN", "DISABLED", "AGE"}),
		loading: true,
		input:   ti,
	}
}

// IsFiltering returns true if the table is in filter mode.
func (v *UserListView) IsFiltering() bool {
	return v.table.Filtering()
}

// IsInActionMode returns true when a user action prompt is active.
func (v *UserListView) IsInActionMode() bool {
	return v.mode != userModeNormal
}

// Init fetches user data.
func (v UserListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v UserListView) Update(msg tea.Msg) (UserListView, tea.Cmd) {
	switch msg := msg.(type) {
	case userListMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.table.SetRows(msg.rows)
		}
		return v, nil

	case userActionResultMsg:
		v.loading = false
		if msg.err != nil {
			v.mode = userModeResult
			v.resultMsg = msg.err.Error()
			v.resultIsError = true
		} else {
			v.mode = userModeResult
			v.resultMsg = v.userActionSuccessMessage()
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
		case userModeInput:
			return v.updateInputMode(msg)
		case userModeConfirm:
			return v.updateConfirmMode(msg)
		case userModeResult:
			return v.updateResultMode(msg)
		default:
			return v.updateNormalMode(msg)
		}
	}
	return v, nil
}

// updateNormalMode handles keys when no action is active.
func (v UserListView) updateNormalMode(msg tea.KeyMsg) (UserListView, tea.Cmd) {
	if v.table.Filtering() {
		cmd := v.table.Update(msg)
		return v, cmd
	}
	switch msg.String() {
	case "r":
		v.loading = true
		return v, v.fetch()
	case "c":
		if !v.Admin {
			return v, nil
		}
		v.mode = userModeInput
		v.pendingAction = userActionCreate
		v.createStep = userCreateStepEmail
		v.createUserEmail = ""
		v.input.Reset()
		v.input.Placeholder = "Email address: "
		v.input.Focus()
		return v, nil
	case "d":
		if !v.Admin {
			return v, nil
		}
		row := v.table.SelectedRow()
		if row != nil && len(row) > 0 {
			v.mode = userModeConfirm
			v.pendingAction = userActionDelete
			v.confirmMsg = fmt.Sprintf("Delete user %s? (y/n)", row[0])
			return v, nil
		}
	case "x":
		if !v.Admin {
			return v, nil
		}
		row := v.table.SelectedRow()
		if row != nil && len(row) > 0 {
			userName := row[0]
			disabledStr := ""
			if len(row) > 5 {
				disabledStr = row[5]
			}
			action := "Disable"
			if disabledStr == "Yes" {
				action = "Enable"
			}
			v.mode = userModeConfirm
			v.pendingAction = userActionToggle
			v.confirmMsg = fmt.Sprintf("%s user %s? (y/n)", action, userName)
			return v, nil
		}
	default:
		cmd := v.table.Update(msg)
		return v, cmd
	}
	return v, nil
}

// updateInputMode handles keys during text input.
func (v UserListView) updateInputMode(msg tea.KeyMsg) (UserListView, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.mode = userModeNormal
		v.pendingAction = userActionNone
		v.input.Blur()
		return v, nil
	case "enter":
		value := strings.TrimSpace(v.input.Value())
		if value == "" {
			v.mode = userModeNormal
			v.pendingAction = userActionNone
			v.input.Blur()
			return v, nil
		}

		if v.createStep == userCreateStepEmail {
			v.createUserEmail = value
			v.createStep = userCreateStepDisplayName
			v.input.Reset()
			v.input.Placeholder = "Display name: "
			v.input.Focus()
			return v, nil
		}

		// Step 1: display name collected, execute
		v.input.Blur()
		v.loading = true

		email := v.createUserEmail
		displayName := value

		return v, func() tea.Msg {
			return v.doCreateUser(email, displayName)
		}
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return v, cmd
	}
}

// updateConfirmMode handles keys when a y/n prompt is active.
func (v UserListView) updateConfirmMode(msg tea.KeyMsg) (UserListView, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		v.loading = true
		row := v.table.SelectedRow()
		userName := ""
		if row != nil && len(row) > 0 {
			userName = row[0]
		}
		action := v.pendingAction
		return v, func() tea.Msg {
			switch action {
			case userActionDelete:
				return v.doDeleteUser(userName)
			case userActionToggle:
				return v.doToggleUser(userName)
			}
			return userActionResultMsg{err: fmt.Errorf("unknown action")}
		}
	case "n", "N", "esc":
		v.mode = userModeNormal
		v.pendingAction = userActionNone
		return v, nil
	}
	return v, nil
}

// updateResultMode handles keys when showing a result message.
func (v UserListView) updateResultMode(msg tea.KeyMsg) (UserListView, tea.Cmd) {
	v.mode = userModeNormal
	v.pendingAction = userActionNone
	v.resultMsg = ""
	v.loading = true
	return v, v.fetch()
}

// doCreateUser creates a new User CRD.
func (v *UserListView) doCreateUser(email, displayName string) userActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	// Derive resource name from email: replace @ and . with -
	name := strings.ReplaceAll(email, "@", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ToLower(name)

	user := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "User",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"email":       email,
				"displayName": displayName,
				"authType":    "internal",
			},
		},
	}

	_, err := v.client.Dynamic.Resource(client.UserGVR).Create(ctx, user, metav1.CreateOptions{})
	if err != nil {
		return userActionResultMsg{err: fmt.Errorf("creating user: %w", err), action: userActionCreate}
	}

	return userActionResultMsg{action: userActionCreate}
}

// doDeleteUser deletes a User CRD.
func (v *UserListView) doDeleteUser(name string) userActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	err := v.client.Dynamic.Resource(client.UserGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return userActionResultMsg{err: fmt.Errorf("deleting user: %w", err), action: userActionDelete}
	}

	return userActionResultMsg{action: userActionDelete}
}

// doToggleUser toggles the disabled field on a User CRD.
func (v *UserListView) doToggleUser(name string) userActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	user, err := v.client.Dynamic.Resource(client.UserGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return userActionResultMsg{err: fmt.Errorf("getting user %s: %w", name, err), action: userActionToggle}
	}

	currentlyDisabled := client.GetNestedBool(user.Object, "spec", "disabled")
	newDisabled := !currentlyDisabled

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"disabled": newDisabled,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return userActionResultMsg{err: fmt.Errorf("marshaling patch: %w", err), action: userActionToggle}
	}

	_, err = v.client.Dynamic.Resource(client.UserGVR).Patch(
		ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
	)
	if err != nil {
		return userActionResultMsg{err: fmt.Errorf("toggling user: %w", err), action: userActionToggle}
	}

	return userActionResultMsg{action: userActionToggle}
}

// userActionSuccessMessage returns a human-readable success message for the current action.
func (v *UserListView) userActionSuccessMessage() string {
	switch v.pendingAction {
	case userActionCreate:
		return "User created. Press any key to refresh."
	case userActionDelete:
		return "User deleted. Press any key to refresh."
	case userActionToggle:
		return "User updated. Press any key to refresh."
	}
	return "Action completed. Press any key to continue."
}

// KeyLegend returns the action keys available for the user view.
func (v *UserListView) KeyLegend() string {
	dimStyle := styles.DimStyle
	keyStyle := styles.KeyLegendStyle

	legend := dimStyle.Render("  ") +
		keyStyle.Render("j/k") + dimStyle.Render(":navigate  ")

	if v.Admin {
		legend += keyStyle.Render("c") + dimStyle.Render(":create  ") +
			keyStyle.Render("d") + dimStyle.Render(":delete  ") +
			keyStyle.Render("x") + dimStyle.Render(":disable/enable  ")
	}

	legend += keyStyle.Render("/") + dimStyle.Render(":filter  ") +
		keyStyle.Render("r") + dimStyle.Render(":refresh  ") +
		keyStyle.Render("?") + dimStyle.Render(":help  ") +
		keyStyle.Render("q") + dimStyle.Render(":quit")

	return legend
}

// View renders the user list.
func (v UserListView) View() string {
	if v.loading {
		return "  Loading users..."
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
func (v UserListView) renderActionPrompt() string {
	switch v.mode {
	case userModeInput:
		return styles.ActionPromptStyle.Render(v.input.Placeholder) + v.input.View()
	case userModeConfirm:
		return styles.ActionConfirmStyle.Render(v.confirmMsg)
	case userModeResult:
		if v.resultIsError {
			return styles.ActionErrorStyle.Render("Error: " + v.resultMsg)
		}
		return styles.ActionSuccessStyle.Render(v.resultMsg)
	}
	return ""
}

func (v UserListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		ctx, cancel := apiContext()
		defer cancel()
		list, err := c.Dynamic.Resource(client.UserGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return userListMsg{err: fmt.Errorf("listing Users: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, usr := range list.Items {
			adminStr := ""
			if client.GetNestedBool(usr.Object, "spec", "isPlatformAdmin") {
				adminStr = "Yes"
			}
			disabledStr := ""
			if client.GetNestedBool(usr.Object, "spec", "disabled") {
				disabledStr = "Yes"
			}
			rows[i] = []string{
				usr.GetName(),
				client.GetNestedString(usr.Object, "spec", "email"),
				client.GetNestedString(usr.Object, "status", "phase"),
				client.GetNestedString(usr.Object, "spec", "authType"),
				adminStr,
				disabledStr,
				output.FormatAge(usr.GetCreationTimestamp().Time),
			}
		}
		return userListMsg{rows: rows}
	}
}
