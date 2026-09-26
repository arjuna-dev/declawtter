package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"declaw/internal/activity"
	"declaw/internal/conversations"
	"declaw/internal/ui"
)

// checkoutChat lists recent conversations and resumes one, optionally in a
// different harness than the one it started in.
//
// Same-harness resume uses each harness's native resume flag so nothing is lost
// in translation. Cross-harness resume renders the conversation into a portable
// transcript and injects it as the opening prompt of a fresh session, which is
// the only safe option: harness session formats are private, mutually
// incompatible, and must never be written by declaw.
func (a *App) checkoutChat(args []string) (string, error) {
	if len(args) == 0 {
		return a.listChats()
	}
	if len(args) > 3 {
		return "", errors.New("usage: declaw checkout chat <id> [continue|change-harness <harness>]")
	}

	decorated, err := a.visibleChats()
	if err != nil {
		return "", err
	}
	selected, err := selectChat(decorated, args[0])
	if err != nil {
		return "", err
	}

	targetHarness := selected.Harness
	crossHarness := false
	if len(args) >= 2 {
		switch strings.ToLower(strings.TrimSpace(args[1])) {
		case "continue":
		case "change-harness", "harness":
			if len(args) != 3 {
				return harnessListOutput("usage: declaw checkout chat <id> change-harness <harness>"), nil
			}
			requested, err := activity.NormalizeHarness(args[2])
			if err != nil {
				return "", err
			}
			if requested == "" {
				return "", errors.New("checkout harness must be one of " + strings.Join(activity.SupportedHarnesses(), ", "))
			}
			targetHarness = requested
			crossHarness = requested != selected.Harness
		default:
			return "", fmt.Errorf("unknown checkout chat action %q; use continue or change-harness", args[1])
		}
	}

	program, cmdArgs, err := a.chatCommand(selected, targetHarness, crossHarness)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(program); err != nil {
		return "", fmt.Errorf("%s command not found in PATH", program)
	}

	store, err := conversations.NewStore()
	if err != nil {
		return "", err
	}
	if err := store.RecordCheckout(selected.Key(), string(targetHarness), time.Now().UTC()); err != nil {
		return "", err
	}
	// Continuing a chat is also project activity, so the project list stays in
	// sync with what the user actually worked on.
	if selected.Path != "" {
		_ = a.projects.RecordActivityAtPath(selected.Path, string(targetHarness), time.Now().UTC(), "declaw checkout chat")
	}

	command := exec.Command(program, cmdArgs...)
	if selected.Path != "" {
		if info, err := os.Stat(selected.Path); err == nil && info.IsDir() {
			command.Dir = selected.Path
		}
	}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()
	return "", command.Run()
}

// chatCommand resolves how to re-enter a conversation. Native resume is used
// whenever the target harness is the origin harness and that harness supports
// resuming by id; otherwise the transcript is injected as a prompt.
func (a *App) chatCommand(selected conversations.Decorated, targetHarness activity.Harness, crossHarness bool) (string, []string, error) {
	registry := conversations.DefaultRegistry()
	if !crossHarness {
		if reader, ok := registry.ReaderFor(selected.Harness); ok {
			if resumer, ok := reader.(conversations.Resumer); ok {
				program, args, err := resumer.ResumeCommand(selected.Conversation)
				if err == nil {
					return program, args, nil
				}
			}
		}
	}

	transcript, err := registry.Load(selected.Conversation)
	if err != nil {
		return "", nil, fmt.Errorf("could not read the conversation transcript: %w", err)
	}
	if len(transcript.Messages) == 0 {
		return "", nil, errors.New("the conversation has no readable messages to carry over")
	}
	prompt := conversations.HandoffPrompt(transcript, string(targetHarness))
	reasoningMode, err := a.effectiveCodexReasoningMode()
	if err != nil {
		return "", nil, err
	}
	return agentCommand(string(targetHarness), prompt, reasoningMode)
}

// selectChat resolves a user-supplied reference: a 1-based list index, a full
// or partial session id, or a harness:id key.
func selectChat(decorated []conversations.Decorated, reference string) (conversations.Decorated, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return conversations.Decorated{}, errors.New("a conversation id or list number is required")
	}
	if index, err := strconv.Atoi(reference); err == nil {
		if index < 1 || index > len(decorated) {
			return conversations.Decorated{}, fmt.Errorf("no conversation numbered %d; run declaw checkout chat to list them", index)
		}
		return decorated[index-1], nil
	}
	lowered := strings.ToLower(reference)
	var matches []conversations.Decorated
	for _, item := range decorated {
		if strings.ToLower(item.Key()) == lowered || strings.ToLower(item.ID) == lowered {
			return item, nil
		}
		if strings.HasPrefix(strings.ToLower(item.ID), lowered) {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return conversations.Decorated{}, fmt.Errorf("no conversation matching %q", reference)
	case 1:
		return matches[0], nil
	default:
		return conversations.Decorated{}, fmt.Errorf("%q matches %d conversations; use a longer id", reference, len(matches))
	}
}

// chatListLimit bounds the default listing. Users with years of history do not
// want several hundred lines dumped into a terminal.
const chatListLimit = 30

func (a *App) allChats() ([]conversations.Decorated, error) {
	registry := conversations.DefaultRegistry()
	// A failing adapter must not hide the harnesses that did load, so listing
	// errors are tolerated here and surfaced only when nothing was found.
	list, listErr := registry.List()
	store, err := conversations.NewStore()
	if err != nil {
		return nil, err
	}
	stored, err := store.All()
	if err != nil {
		return nil, err
	}
	decorated := conversations.Decorate(list, stored)
	if len(decorated) == 0 && listErr != nil {
		return nil, listErr
	}
	return decorated, nil
}

func (a *App) visibleChats() ([]conversations.Decorated, error) {
	decorated, err := a.allChats()
	if err != nil {
		return nil, err
	}
	return conversations.Visible(decorated), nil
}

func (a *App) listChats() (string, error) {
	visible, err := a.visibleChats()
	if err != nil {
		return "", err
	}
	if len(visible) == 0 {
		return "no available conversations", nil
	}
	limit := len(visible)
	if limit > chatListLimit {
		limit = chatListLimit
	}
	lines := make([]string, 0, limit+1)
	for index, item := range visible[:limit] {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, formatChatLine(item)))
	}
	if len(visible) > limit {
		lines = append(lines, fmt.Sprintf("… and %d more", len(visible)-limit))
	}
	return strings.Join(lines, "\n"), nil
}

func formatChatLine(item conversations.Decorated) string {
	markers := ""
	if item.Settings.Pinned {
		markers += "* "
	}
	when := "never used"
	if !item.UpdatedAt.IsZero() {
		when = item.UpdatedAt.Local().Format("2006-01-02 15:04")
	}
	details := []string{displayHarness(string(item.Harness)), when}
	if item.Path != "" {
		details = append(details, item.Path)
	}
	if item.Compacted {
		details = append(details, "compacted")
	}
	return fmt.Sprintf("%s%s [%s] (%s)", markers, item.DisplayTitle(), shortChatID(item.ID), strings.Join(details, " - "))
}

// shortChatID trims a session UUID to a prefix that is still unambiguous in
// practice but readable in a list.
func shortChatID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// chatSettings implements `declaw chat-settings`, mirroring project-settings so
// conversations can be pinned, ignored and renamed the same way projects are.
func (a *App) chatSettings(args []string) (string, error) {
	if len(args) == 0 {
		return a.listAllChatSettings()
	}
	decorated, err := a.allChats()
	if err != nil {
		return "", err
	}
	selected, err := selectChat(decorated, args[0])
	if err != nil {
		return "", err
	}
	store, err := conversations.NewStore()
	if err != nil {
		return "", err
	}
	if len(args) == 1 {
		return formatChatSettings(selected), nil
	}

	action := strings.ToLower(strings.TrimSpace(args[1]))
	value := ""
	if len(args) >= 3 {
		value = strings.TrimSpace(strings.Join(args[2:], " "))
	}
	switch action {
	case "pin":
		on, err := parseToggle(value, true)
		if err != nil {
			return "", err
		}
		if _, err := store.SetPinned(selected.Key(), on); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s pin %s", selected.DisplayTitle(), toggleWord(on)), nil
	case "ignore":
		on, err := parseToggle(value, true)
		if err != nil {
			return "", err
		}
		if _, err := store.SetIgnored(selected.Key(), on); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s ignore %s", selected.DisplayTitle(), toggleWord(on)), nil
	case "give", "alias", "rename":
		alias := value
		if action == "give" {
			alias = strings.TrimSpace(strings.TrimPrefix(value, "alias"))
		}
		if alias == "" {
			return "", errors.New("usage: declaw chat-settings <id> alias <display name>")
		}
		if _, err := store.SetAlias(selected.Key(), alias); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s renamed to %s", shortChatID(selected.ID), alias), nil
	default:
		return "", fmt.Errorf("unknown chat setting %q; use pin, ignore or alias", args[1])
	}
}

func (a *App) listAllChatSettings() (string, error) {
	decorated, err := a.allChats()
	if err != nil {
		return "", err
	}
	if len(decorated) == 0 {
		return "no available conversations", nil
	}
	limit := len(decorated)
	if limit > chatListLimit {
		limit = chatListLimit
	}
	lines := make([]string, 0, limit)
	for index, item := range decorated[:limit] {
		state := ""
		if item.Settings.Ignored {
			state = " (ignored)"
		}
		lines = append(lines, fmt.Sprintf("%d. %s%s", index+1, formatChatLine(item), state))
	}
	if len(decorated) > limit {
		lines = append(lines, fmt.Sprintf("… and %d more", len(decorated)-limit))
	}
	return strings.Join(lines, "\n"), nil
}

func formatChatSettings(item conversations.Decorated) string {
	lines := []string{
		"title: " + item.DisplayTitle(),
		"id: " + item.ID,
		"harness: " + displayHarness(string(item.Harness)),
		"pinned: " + toggleWord(item.Settings.Pinned),
		"ignored: " + toggleWord(item.Settings.Ignored),
	}
	if item.Path != "" {
		lines = append(lines, "path: "+item.Path)
	}
	if item.Compacted {
		lines = append(lines, "compacted: on")
	}
	return strings.Join(lines, "\n")
}

func parseToggle(value string, defaultOn bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultOn, nil
	case "on", "true", "yes":
		return true, nil
	case "off", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected on or off, got %q", value)
	}
}

func toggleWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// launcherChatLimit bounds how many conversations appear in the interactive
// launcher. The list is navigated with arrow keys, so it must stay shallow.
const launcherChatLimit = 15

// checkoutChatCommandChildren builds the `/checkout chat` submenu: one entry per
// recent conversation, each offering continue or change-harness.
func (a *App) checkoutChatCommandChildren() ([]ui.Command, error) {
	visible, err := a.visibleChats()
	if err != nil {
		return nil, err
	}
	if len(visible) > launcherChatLimit {
		visible = visible[:launcherChatLimit]
	}
	children := make([]ui.Command, 0, len(visible))
	for _, item := range visible {
		harness := displayHarness(string(item.Harness))
		// The command line uses the stable session id while the menu shows the
		// human title, so selecting an entry produces a reproducible command.
		displayPrefix := "/checkout chat " + item.DisplayTitle()
		commandPrefix := "/checkout chat " + item.ID
		children = append(children, ui.Command{
			Name:        displayPrefix,
			CommandLine: commandPrefix,
			Description: chatMenuDescription(item),
			Children: []ui.Command{
				{Name: displayPrefix + " continue", CommandLine: commandPrefix + " continue", Description: "Continue with " + harness},
				{
					Name:        displayPrefix + " change-harness",
					CommandLine: commandPrefix + " change-harness",
					Description: "Continue this conversation in another harness",
					Children:    harnessCommandChildrenWithPrefixes(displayPrefix+" change-harness", commandPrefix+" change-harness", "Carry the transcript over"),
				},
			},
		})
	}
	return children, nil
}

// chatSettingsCommandChildren builds the `/chat-settings` submenu with pin,
// ignore and alias actions per conversation.
func (a *App) chatSettingsCommandChildren() ([]ui.Command, error) {
	all, err := a.allChats()
	if err != nil {
		return nil, err
	}
	if len(all) > launcherChatLimit {
		all = all[:launcherChatLimit]
	}
	children := make([]ui.Command, 0, len(all))
	for _, item := range all {
		displayPrefix := "/chat-settings " + item.DisplayTitle()
		commandPrefix := "/chat-settings " + item.ID
		pinValue, pinDescription := "on", "Pin conversation to the top"
		if item.Settings.Pinned {
			pinValue, pinDescription = "off", "Unpin conversation from the top"
		}
		ignoreValue, ignoreDescription := "on", "Hide conversation from checkout"
		if item.Settings.Ignored {
			ignoreValue, ignoreDescription = "off", "Show conversation in checkout"
		}
		children = append(children, ui.Command{
			Name:        displayPrefix,
			CommandLine: commandPrefix,
			Description: chatMenuDescription(item),
			Children: []ui.Command{
				{Name: displayPrefix + " pin " + pinValue, CommandLine: commandPrefix + " pin " + pinValue, Description: pinDescription},
				{Name: displayPrefix + " ignore " + ignoreValue, CommandLine: commandPrefix + " ignore " + ignoreValue, Description: ignoreDescription},
				{Name: displayPrefix + " alias", CommandLine: commandPrefix + " alias ", Description: "Give the conversation a display name"},
			},
		})
	}
	return children, nil
}

func chatMenuDescription(item conversations.Decorated) string {
	parts := []string{displayHarness(string(item.Harness))}
	if !item.UpdatedAt.IsZero() {
		parts = append(parts, item.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	if item.Path != "" {
		parts = append(parts, item.Path)
	}
	if item.Compacted {
		parts = append(parts, "compacted")
	}
	if item.Settings.Pinned {
		parts = append(parts, "pinned")
	}
	if item.Settings.Ignored {
		parts = append(parts, "ignored")
	}
	return strings.Join(parts, " - ")
}
