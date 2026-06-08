package context

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3"

	"babyagent/shared"
)

type Summarizer interface {
	GetSummaryInputTokenLimit() int
	Summarize(ctx context.Context, runningSummary string, messages []shared.OpenAIMessage) (string, error)
}

const (
	summaryPromptTemplate = `Summarize the following conversation history between a user and an AI assistant.

<previous_summary>
{previous_summary}
</previous_summary>

<conversation>
{text}
</conversation>

Requirements:
- Preserve key information: user requests, tool calls, and important results
- Keep the summary under {summary_length} words
- Output ONLY the summary, no explanations
- Use concise language, omit redundant details

Example:

Input:
user: What files are in the current directory?
assistant: I'll use the bash tool to list files.
tool: file1.txt file2.go directory/
assistant: The directory contains file1.txt, file2.go, and a directory/.

Output:
User asked to list directory contents. Assistant ran bash command showing file1.txt, file2.go, and a directory.
`
)

type LLMSummarizer struct {
	llmClient        openai.Client
	modelConf        shared.ModelConfig
	summaryCharLimit int
}

func (s *LLMSummarizer) GetSummaryInputTokenLimit() int {
	return s.modelConf.ContextWindow / 2
}

func NewLLMSummarizer(modelConf shared.ModelConfig, summaryCharLimit int) *LLMSummarizer {
	return &LLMSummarizer{
		llmClient:        shared.NewLLMClient(modelConf),
		modelConf:        modelConf,
		summaryCharLimit: summaryCharLimit,
	}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, runningSummary string, messages []shared.OpenAIMessage) (string, error) {
	var b strings.Builder

	for i := range messages {
		roleName := shared.GetRoleName(messages[i])
		contentAny := messages[i].GetContent().AsAny()
		contentStr, ok := contentAny.(*string)
		if !ok {
			continue
		}
		b.WriteString(roleName)
		b.WriteString(": ")
		b.WriteString(*contentStr)
		b.WriteString("\n")
	}

	prompt := strings.ReplaceAll(summaryPromptTemplate, "{text}", b.String())
	prompt = strings.ReplaceAll(prompt, "{previous_summary}", runningSummary)
	prompt = strings.ReplaceAll(prompt, "{summary_length}", strconv.Itoa(s.summaryCharLimit))

	resp, err := s.llmClient.Chat.Completions.New(ctx,
		openai.ChatCompletionNewParams{
			Model: s.modelConf.Model,
			Messages: []shared.OpenAIMessage{
				openai.UserMessage(prompt),
			},
		},
	)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("no choices returned")
	}
	return resp.Choices[0].Message.Content, nil

}
