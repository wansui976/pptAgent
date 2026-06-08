package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// fallbackHostPPTMasterRoot 用于无显式配置 ppt-master 路径时回退；
// 与 docker bash 容器挂载的 /workspace/ppt-master 对齐。
const fallbackHostPPTMasterRoot = "/workspace/ppt-master"

type BashTool struct {
	pptMasterRoot string
}

func NewBashTool() *BashTool {
	return &BashTool{pptMasterRoot: fallbackHostPPTMasterRoot}
}

func NewBashToolWithRoot(pptMasterRoot string) *BashTool {
	if pptMasterRoot == "" {
		pptMasterRoot = fallbackHostPPTMasterRoot
	}
	return &BashTool{pptMasterRoot: pptMasterRoot}
}

type BashToolParam struct {
	Command string `json:"command"`
}

func (t *BashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *BashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute",
				},
			},
			"required": []string{"command"},
		},
	})
}

func (t *BashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := BashToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}
	if err := validateShellCommand(p.Command); err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: use cmd.exe to interpret the command line
		cmd = exec.CommandContext(ctx, "cmd", "/C", p.Command)
	} else {
		// Linux/macOS: use POSIX sh (more universal than assuming bash exists)
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}
	t.configureHostPythonEnv(cmd, p.Command)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return "", fmt.Errorf("%w: %s", err, string(output))
		}
		return "", err
	}
	return string(output), nil
}

func validateShellCommand(command string) error {
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	forbiddenFragments := []string{
		"sudo ",
		" su ",
		"docker ",
		"podman ",
		"launchctl ",
		"systemctl ",
		"mkfs",
		":(){",
		"/etc/passwd",
		"/etc/shadow",
		".ssh/",
		"id_rsa",
		"id_ed25519",
	}
	for _, fragment := range forbiddenFragments {
		if strings.Contains(normalized, fragment) {
			return fmt.Errorf("blocked unsafe shell command fragment: %s", fragment)
		}
	}
	rootDelete := regexp.MustCompile(`(^|[;&|]\s*)rm\s+(-[a-z]*[rf][a-z]*\s+)+(/|\*)($|\s|[;&|])`)
	if rootDelete.MatchString(normalized) {
		return fmt.Errorf("blocked destructive shell command")
	}
	return nil
}

func (t *BashTool) configureHostPythonEnv(cmd *exec.Cmd, command string) {
	if cmd == nil || !shouldPreparePPTMasterPython(command) {
		return
	}

	venvBin := filepath.Join(t.pptMasterRoot, ".venv", "bin")
	venvPython := filepath.Join(venvBin, "python")
	if _, err := os.Stat(venvPython); err != nil {
		return
	}

	currentPath := os.Getenv("PATH")
	env := os.Environ()
	env = append(env,
		"VIRTUAL_ENV="+filepath.Join(t.pptMasterRoot, ".venv"),
		"PATH="+venvBin+string(os.PathListSeparator)+currentPath,
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
	)
	cmd.Env = env

	if !requiresPPTMasterBootstrap(command) {
		return
	}

	bootstrap := fmt.Sprintf(
		"if [ -x %q ]; then %q -c 'import requests, bs4' >/dev/null 2>&1 || %q -m pip install -q requests beautifulsoup4 pillow; fi",
		venvPython,
		venvPython,
		venvPython,
	)

	if runtime.GOOS == "windows" {
		return
	}
	cmd.Args = []string{"sh", "-c", bootstrap + " && " + command}
}

func shouldPreparePPTMasterPython(command string) bool {
	normalized := strings.ToLower(command)
	return strings.Contains(normalized, "ppt-master") ||
		strings.Contains(normalized, "python ") ||
		strings.Contains(normalized, "python3 ") ||
		strings.Contains(normalized, ".py")
}

func requiresPPTMasterBootstrap(command string) bool {
	normalized := strings.ToLower(command)
	return strings.Contains(normalized, "ppt-master") ||
		strings.Contains(normalized, "source_to_md") ||
		strings.Contains(normalized, "web_to_md.py") ||
		strings.Contains(normalized, "generate_ppt.py")
}
