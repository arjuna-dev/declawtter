package conversations

import (
	"fmt"
	"strings"
)

// Rendering budget for a handoff prompt. Harnesses differ in context size, so
// this stays conservative: the goal is to carry the thread of the conversation,
// not to replay every token. When a transcript exceeds the budget the oldest
// middle turns are dropped and an explicit elision marker is inserted, because
// silently truncating context produces confidently wrong continuations.
const (
	maxPromptCharacters  = 24000
	maxMessageCharacters = 4000
	headMessages         = 4
	tailMessages         = 20
)

// Render turns a transcript into the plain-text conversation body.
func Render(transcript Transcript) string {
	messages := budgetMessages(transcript.Messages)
	var builder strings.Builder
	for _, message := range messages {
		if message.Elision != "" {
			builder.WriteString("\n[" + message.Elision + "]\n\n")
			continue
		}
		builder.WriteString(strings.ToUpper(message.Role))
		builder.WriteString(":\n")
		builder.WriteString(clampText(message.Content, maxMessageCharacters))
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

// renderedMessage is a Message plus an optional elision marker used when the
// transcript had to be shortened to fit the prompt budget.
type renderedMessage struct {
	Message
	Elision string
}

func budgetMessages(messages []Message) []renderedMessage {
	rendered := make([]renderedMessage, 0, len(messages))
	total := 0
	for _, message := range messages {
		total += len(message.Content)
		rendered = append(rendered, renderedMessage{Message: message})
	}
	if total <= maxPromptCharacters || len(messages) <= headMessages+tailMessages {
		return rendered
	}
	dropped := len(messages) - headMessages - tailMessages
	out := make([]renderedMessage, 0, headMessages+tailMessages+1)
	out = append(out, rendered[:headMessages]...)
	out = append(out, renderedMessage{
		Elision: fmt.Sprintf("%d earlier messages omitted to fit the new session's context", dropped),
	})
	out = append(out, rendered[len(rendered)-tailMessages:]...)
	return out
}

func clampText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "\n[… message truncated …]"
}

// HandoffPrompt builds the prompt injected when continuing a conversation in a
// different harness. It states plainly that the transcript is history from
// another agent so the new harness does not mistake it for a fresh instruction,
// and flags compaction so the model knows detail was already condensed.
func HandoffPrompt(transcript Transcript, targetHarness string) string {
	conversation := transcript.Conversation
	var builder strings.Builder
	builder.WriteString("You are continuing a conversation that was started in a different agent harness.\n\n")
	builder.WriteString("Origin harness: " + string(conversation.Harness) + "\n")
	builder.WriteString("Continuing in: " + strings.TrimSpace(targetHarness) + "\n")
	if conversation.Path != "" {
		builder.WriteString("Working directory: " + conversation.Path + "\n")
	}
	if !conversation.UpdatedAt.IsZero() {
		builder.WriteString("Last active: " + conversation.UpdatedAt.Local().Format("2006-01-02 15:04") + "\n")
	}
	if transcript.Compacted {
		builder.WriteString("Note: the origin harness compacted this conversation, so the transcript below is its own condensed summary rather than the full turn-by-turn history.\n")
	}
	builder.WriteString("\nThe transcript below is prior history for context. Do not respond to it as if it were a new request; wait for the user's next instruction.\n\n")
	builder.WriteString("--- BEGIN TRANSCRIPT ---\n")
	builder.WriteString(Render(transcript))
	builder.WriteString("\n--- END TRANSCRIPT ---\n")
	return builder.String()
}
