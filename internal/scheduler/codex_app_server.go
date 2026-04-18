package scheduler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type codexAppServerClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  io.Closer
	scanner *bufio.Scanner
	nextID  int
}

type codexAppServerMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type appServerTurnState struct {
	threadID string
	turnID   string
	messages []string
	statuses []string
	failure  string
}

func runCodexAppServerChat(prompt, workspace, jobName string, extraEnv map[string]string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("missing Codex prompt")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("codex command not found in PATH")
	}
	logPath := filepath.Join(os.TempDir(), "declaw-codex-app-server.err.log")
	if runDir := envValue(extraEnv, "DECLAW_RUN_DIR"); strings.TrimSpace(runDir) != "" {
		logPath = filepath.Join(runDir, "codex-app-server.err.log")
	}

	fmt.Println("declaw")
	fmt.Println()

	agentName := declawAgentName(workspace)
	stateless := codexAppServerStateless(extraEnv)
	if !stateless {
		printRecentDeclawChatHistory(workspace, agentName, declawChatHistoryLimit)
	}

	transcriptPath := ""
	if !stateless {
		var err error
		transcriptPath, err = startDeclawChatTranscript(workspace, jobName, prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create visible chat transcript: %s\n", err)
		}
	}

	client, err := startCodexAppServer(workspace, extraEnv, logPath)
	if err != nil {
		return err
	}
	defer client.close()

	threadID, err := client.startThread(workspace, stateless)
	if err != nil {
		return err
	}

	displayMessage, err := client.runTurn(threadID, prompt)
	if err != nil {
		return err
	}
	if displayMessage != "" {
		printDeclawChatMessage(agentName, displayMessage)
		if transcriptPath != "" {
			_ = appendDeclawChatMessage(transcriptPath, agentName, displayMessage)
		}
	}

	for {
		input, err := readDeclawChatInput()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		message := input.Message
		if message == "" {
			continue
		}
		redrawUserInput(input.Display)
		switch strings.ToLower(message) {
		case "q", "quit", "exit":
			fmt.Println("bye")
			return nil
		case "/raw":
			fmt.Printf("Raw Codex session: codex resume %s\n", threadID)
			fmt.Printf("Raw log: %s\n", logPath)
			continue
		case "/info":
			if jobName != "" {
				fmt.Printf("Job: %s\n", jobName)
			}
			if workspace != "" {
				fmt.Printf("Workspace: %s\n", workspace)
			}
			fmt.Printf("Raw log: %s\n", logPath)
			if transcriptPath != "" {
				fmt.Printf("Visible chat transcript: %s\n", transcriptPath)
			}
			continue
		}

		if transcriptPath != "" {
			_ = appendDeclawChatMessage(transcriptPath, "User", message)
		}
		displayMessage, err := client.runTurn(threadID, message)
		if err != nil {
			fmt.Fprintf(os.Stderr, "follow-up failed: %s\n", err)
			fmt.Fprintf(os.Stderr, "You can try again, or open the raw session with /raw.\n")
			continue
		}
		if displayMessage != "" {
			printDeclawChatMessage(agentName, displayMessage)
			if transcriptPath != "" {
				_ = appendDeclawChatMessage(transcriptPath, agentName, displayMessage)
			}
		}
	}
}

func startCodexAppServer(workspace string, extraEnv map[string]string, stderrPath string) (*codexAppServerClient, error) {
	cmd := exec.Command(
		"codex",
		"app-server",
		"-c", "shell_environment_policy.inherit=all",
		"--listen", "stdio://",
	)
	if workspace != "" {
		cmd.Dir = workspace
	}
	cmd.Env = mergeEnv(os.Environ(), extraEnv)

	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	cmd.Stderr = stderrFile

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = stderrFile.Close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stderrFile.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stderrFile.Close()
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	client := &codexAppServerClient{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderrFile,
		scanner: scanner,
		nextID:  1,
	}
	if err := client.initialize(); err != nil {
		client.close()
		return nil, err
	}
	return client, nil
}

func (c *codexAppServerClient) initialize() error {
	_, err := c.request("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "declaw",
			"title":   "Declaw",
			"version": "0.0.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
			"optOutNotificationMethods": []string{
				"thread/tokenUsage/updated",
				"account/rateLimits/updated",
			},
		},
	}, nil)
	if err != nil {
		return err
	}
	return c.notify("initialized", nil)
}

func (c *codexAppServerClient) startThread(workspace string, stateless bool) (string, error) {
	params := map[string]any{
		"serviceName":            "declaw",
		"approvalPolicy":         "on-request",
		"sandbox":                "danger-full-access",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": !stateless,
		"ephemeral":              stateless,
	}
	if strings.TrimSpace(workspace) != "" {
		params["cwd"] = workspace
	}
	result, err := c.request("thread/start", params, nil)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return "", err
	}
	if parsed.Thread.ID == "" {
		return "", errors.New("codex app-server did not return a thread id")
	}
	return parsed.Thread.ID, nil
}

func codexAppServerStateless(extraEnv map[string]string) bool {
	return envValue(extraEnv, "DECLAW_CODEX_STATELESS") == "1"
}

func (c *codexAppServerClient) runTurn(threadID, prompt string) (string, error) {
	state := &appServerTurnState{threadID: threadID}
	result, err := c.request("turn/start", map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type":          "text",
			"text":          prompt,
			"text_elements": []any{},
		}},
	}, state)
	if err != nil {
		return "", err
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(result, &started); err != nil {
		return "", err
	}
	if started.Turn.ID == "" {
		return "", errors.New("codex app-server did not return a turn id")
	}
	state.turnID = started.Turn.ID

	showSpinner := stdoutIsTerminal()
	done := make(chan struct{})
	if showSpinner {
		go runDeclawSpinner(done)
	}
	err = c.readUntilTurnComplete(state)
	if showSpinner {
		close(done)
		clearDeclawSpinner()
	}
	if err != nil {
		return "", err
	}
	if state.failure != "" {
		return "", errors.New(state.failure)
	}
	return selectDeclawDisplayMessage(state.messages), nil
}

func (c *codexAppServerClient) request(method string, params any, state *appServerTurnState) (json.RawMessage, error) {
	id := c.nextID
	c.nextID++
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	if err := c.write(msg); err != nil {
		return nil, err
	}
	for c.scanner.Scan() {
		var incoming codexAppServerMessage
		if err := json.Unmarshal(c.scanner.Bytes(), &incoming); err != nil {
			continue
		}
		if len(incoming.ID) > 0 && incoming.Method != "" {
			if err := c.handleServerRequest(incoming); err != nil {
				return nil, err
			}
			continue
		}
		if incoming.Method != "" {
			c.handleNotification(incoming.Method, incoming.Params, state)
			continue
		}
		if idMatches(incoming.ID, id) {
			if incoming.Error != nil {
				return nil, fmt.Errorf("codex app-server %s failed: %s", method, incoming.Error.Message)
			}
			return incoming.Result, nil
		}
	}
	if err := c.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

func (c *codexAppServerClient) notify(method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	return c.write(msg)
}

func (c *codexAppServerClient) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.stdin.Write(data)
	return err
}

func (c *codexAppServerClient) readUntilTurnComplete(state *appServerTurnState) error {
	for c.scanner.Scan() {
		var incoming codexAppServerMessage
		if err := json.Unmarshal(c.scanner.Bytes(), &incoming); err != nil {
			continue
		}
		if len(incoming.ID) > 0 && incoming.Method != "" {
			if err := c.handleServerRequest(incoming); err != nil {
				return err
			}
			continue
		}
		if incoming.Method == "" {
			continue
		}
		if c.handleNotification(incoming.Method, incoming.Params, state) {
			return nil
		}
	}
	if err := c.scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func (c *codexAppServerClient) handleNotification(method string, params json.RawMessage, state *appServerTurnState) bool {
	if state == nil {
		return false
	}
	switch method {
	case "turn/completed":
		var parsed struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &parsed) == nil && parsed.ThreadID == state.threadID && parsed.Turn.ID == state.turnID {
			if parsed.Turn.Status == "failed" {
				if parsed.Turn.Error != nil && parsed.Turn.Error.Message != "" {
					state.failure = parsed.Turn.Error.Message
				} else {
					state.failure = "codex turn failed"
				}
				return true
			}
			return true
		}
	case "item/completed":
		var parsed struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Item     struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Phase string `json:"phase"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &parsed) == nil && parsed.ThreadID == state.threadID && sameOrUnknownTurn(state.turnID, parsed.TurnID) {
			if parsed.Item.Type == "agentMessage" && strings.TrimSpace(parsed.Item.Text) != "" {
				state.messages = append(state.messages, strings.TrimSpace(parsed.Item.Text))
			}
		}
	case "item/started":
		c.recordStartedItemStatus(params, state)
	case "error":
		var parsed struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &parsed) == nil && strings.TrimSpace(parsed.Message) != "" {
			state.statuses = append(state.statuses, parsed.Message)
		}
	}
	return false
}

func (c *codexAppServerClient) recordStartedItemStatus(params json.RawMessage, state *appServerTurnState) {
	var parsed struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Item     struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Server  string `json:"server"`
			Tool    string `json:"tool"`
			Query   string `json:"query"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &parsed) != nil || parsed.ThreadID != state.threadID || !sameOrUnknownTurn(state.turnID, parsed.TurnID) {
		return
	}
	switch parsed.Item.Type {
	case "commandExecution":
		if parsed.Item.Command != "" {
			state.statuses = append(state.statuses, "running command: "+parsed.Item.Command)
		}
	case "mcpToolCall":
		label := strings.Trim(parsed.Item.Server+"."+parsed.Item.Tool, ".")
		if label != "" {
			state.statuses = append(state.statuses, "using "+label)
		}
	case "webSearch":
		if parsed.Item.Query != "" {
			state.statuses = append(state.statuses, "searching: "+parsed.Item.Query)
		}
	}
}

func (c *codexAppServerClient) handleServerRequest(msg codexAppServerMessage) error {
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		return c.respond(msg.ID, map[string]any{"decision": promptApprovalDecision(msg.Params, "command approval")})
	case "item/fileChange/requestApproval":
		return c.respond(msg.ID, map[string]any{"decision": promptApprovalDecision(msg.Params, "file change approval")})
	case "mcpServer/elicitation/request":
		return c.handleMcpElicitation(msg)
	case "item/tool/requestUserInput":
		return c.handleToolUserInput(msg)
	case "item/permissions/requestApproval":
		return c.handlePermissionsApproval(msg)
	default:
		return c.respondError(msg.ID, -32601, "declaw does not implement "+msg.Method)
	}
}

func (c *codexAppServerClient) handlePermissionsApproval(msg codexAppServerMessage) error {
	var parsed struct {
		Reason      string         `json:"reason"`
		Permissions map[string]any `json:"permissions"`
	}
	_ = json.Unmarshal(msg.Params, &parsed)
	fmt.Printf("\n%s\n", colorize("permissions approval", ansiDim))
	if parsed.Reason != "" {
		fmt.Printf("Reason: %s\n", parsed.Reason)
	}
	if !stdinIsTerminal() || !askYesNo("Approve requested permissions for this session?") {
		return c.respond(msg.ID, map[string]any{
			"permissions": map[string]any{},
			"scope":       "turn",
		})
	}
	return c.respond(msg.ID, map[string]any{
		"permissions": parsed.Permissions,
		"scope":       "session",
	})
}

func (c *codexAppServerClient) handleMcpElicitation(msg codexAppServerMessage) error {
	var parsed struct {
		ServerName string `json:"serverName"`
		Mode       string `json:"mode"`
		Message    string `json:"message"`
		URL        string `json:"url"`
	}
	_ = json.Unmarshal(msg.Params, &parsed)
	if parsed.Message != "" {
		fmt.Printf("\n%s %s\n", colorize("Codex needs input >", ansiDim), parsed.Message)
	}
	if parsed.URL != "" {
		fmt.Printf("%s %s\n", colorize("Open URL >", ansiDim), parsed.URL)
	}
	if !stdinIsTerminal() {
		return c.respond(msg.ID, map[string]any{"action": "decline", "content": nil, "_meta": nil})
	}
	if askYesNo("Approve this Codex request?") {
		return c.respond(msg.ID, map[string]any{"action": "accept", "content": map[string]any{}, "_meta": nil})
	}
	return c.respond(msg.ID, map[string]any{"action": "decline", "content": nil, "_meta": nil})
}

func (c *codexAppServerClient) handleToolUserInput(msg codexAppServerMessage) error {
	var parsed struct {
		Questions []struct {
			ID       string `json:"id"`
			Question string `json:"question"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(msg.Params, &parsed)
	answers := map[string]any{}
	for _, question := range parsed.Questions {
		if question.ID == "" {
			continue
		}
		answer := ""
		if stdinIsTerminal() {
			fmt.Printf("\n%s %s\n", colorize("Codex asks >", ansiDim), question.Question)
			fmt.Print(colorize("Answer > ", ansiBlue))
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			answer = strings.TrimSpace(line)
		}
		answers[question.ID] = map[string]any{"answers": []string{answer}}
	}
	return c.respond(msg.ID, map[string]any{"answers": answers})
}

func (c *codexAppServerClient) respond(id json.RawMessage, result any) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	})
}

func (c *codexAppServerClient) respondError(id json.RawMessage, code int, message string) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *codexAppServerClient) close() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	if c.stderr != nil {
		_ = c.stderr.Close()
	}
}

func promptApprovalDecision(params json.RawMessage, label string) string {
	var parsed struct {
		Reason  string `json:"reason"`
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	_ = json.Unmarshal(params, &parsed)
	fmt.Printf("\n%s\n", colorize(label, ansiDim))
	if parsed.Reason != "" {
		fmt.Printf("Reason: %s\n", parsed.Reason)
	}
	if parsed.Command != "" {
		fmt.Printf("Command: %s\n", parsed.Command)
	}
	if parsed.Cwd != "" {
		fmt.Printf("Cwd: %s\n", parsed.Cwd)
	}
	if stdinIsTerminal() && askYesNo("Approve?") {
		return "accept"
	}
	return "decline"
}

func askYesNo(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func sameOrUnknownTurn(expected, actual string) bool {
	return expected == "" || actual == "" || expected == actual
}

func idMatches(raw json.RawMessage, id int) bool {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s == fmt.Sprintf("%d", id)
		}
	}
	return value == fmt.Sprintf("%d", id)
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
