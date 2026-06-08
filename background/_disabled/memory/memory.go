package memory

import (
	"context"
	"strings"

	"babyagent/ch09/storage"
	"babyagent/shared"
)

type Memory interface {
	String() string
	Update(ctx context.Context, newMessages []shared.OpenAIMessage) error
}

type MultiLevelMemory struct {
	content MemoryContent

	globalStorage    storage.Storage
	globalKey        string
	workspaceStorage storage.Storage
	workspaceKey     string

	updater MemoryUpdater
}

func NewMultiLevelMemory(globalStorage storage.Storage, workspaceStorage storage.Storage, u MemoryUpdater) *MultiLevelMemory {
	m := &MultiLevelMemory{
		globalStorage:    globalStorage,
		globalKey:        "/memory/MEMORY.md",
		workspaceStorage: workspaceStorage,
		workspaceKey:     "/memory/MEMORY.md",
		updater:          u,
	}
	m.content = m.load()
	return m
}

func (m *MultiLevelMemory) load() MemoryContent {
	ctx := context.Background()
	content := MemoryContent{}

	content.WorkspaceMemory, _ = m.workspaceStorage.Load(ctx, m.workspaceKey)
	content.GlobalMemory, _ = m.globalStorage.Load(ctx, m.globalKey)

	return content
}

func (m *MultiLevelMemory) String() string {
	return m.content.String()
}

func (m *MultiLevelMemory) Update(ctx context.Context, newMessages []shared.OpenAIMessage) error {
	if len(newMessages) == 0 {
		return nil
	}

	newMemory, err := m.updater.Update(ctx, m.content, newMessages)
	if err != nil {
		return err
	}

	if err := m.globalStorage.Store(ctx, m.globalKey, newMemory.GlobalMemory); err != nil {
		return err
	}
	if err := m.workspaceStorage.Store(ctx, m.workspaceKey, newMemory.WorkspaceMemory); err != nil {
		return err
	}

	// 更新内存中的 content
	m.content = newMemory

	return nil
}

type MemoryContent struct {
	GlobalMemory    string `json:"global_memory,omitempty"`
	WorkspaceMemory string `json:"workspace_memory,omitempty"`
}

func (m *MemoryContent) String() string {
	prompt := memoryPromptTemplate
	prompt = strings.ReplaceAll(prompt, "{global_memory}", m.GlobalMemory)
	prompt = strings.ReplaceAll(prompt, "{workspace_memory}", m.WorkspaceMemory)
	return prompt
}

const memoryPromptTemplate = `### Global Memory
Here is the memory about the user among all conversations:
{global_memory}

### Workspace Memory
The memory of the current workspace is:
{workspace_memory}
`
