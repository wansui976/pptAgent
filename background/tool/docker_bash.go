package tool

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	DefaultSandboxContainer = "babyagent-sandbox"
	DefaultSandboxImage     = "lingxi-sandbox:latest"
)

// generateContainerName generates a unique container name based on workspace directory
func generateContainerName(workspaceDir string) string {
	base := sanitizeContainerNamePart(filepath.Base(workspaceDir))
	if base == "" {
		base = "workspace"
	}
	sum := sha1.Sum([]byte(workspaceDir))
	return fmt.Sprintf("%s-%s-%s", DefaultSandboxContainer, base, hex.EncodeToString(sum[:])[:10])
}

func sanitizeContainerNamePart(value string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
	value = re.ReplaceAllString(value, "-")
	value = regexp.MustCompile(`^[^a-zA-Z0-9]+`).ReplaceAllString(value, "")
	if len(value) > 32 {
		return value[:32]
	}
	return value
}

type DockerBashTool struct {
	containerName string
	image         string
	workspaceDir  string
	pptMasterRoot string
	hostTool      Tool

	once     sync.Once
	startErr error
}

func NewDockerBashTool(containerName, workspaceDir string) *DockerBashTool {
	return NewDockerBashToolWithPPTMaster(containerName, workspaceDir, "")
}

func NewDockerBashToolWithPPTMaster(containerName, workspaceDir string, pptMasterRoot string) *DockerBashTool {
	if containerName == "" {
		containerName = generateContainerName(workspaceDir)
	}
	image := os.Getenv("LINGXI_SANDBOX_IMAGE")
	if image == "" {
		image = DefaultSandboxImage
	}
	return &DockerBashTool{
		containerName: containerName,
		image:         image,
		workspaceDir:  workspaceDir,
		pptMasterRoot: pptMasterRoot,
		hostTool:      nil,
	}
}

func (t *DockerBashTool) ToolName() AgentTool {
	return AgentToolBash
}

func (t *DockerBashTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolBash),
		Description: openai.String("execute bash command in a docker sandbox container"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "the bash command to execute in the sandbox",
				},
			},
			"required": []string{"command"},
		},
	})
}

func (t *DockerBashTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	// Lazy initialization: start container on first use
	t.once.Do(func() {
		t.startErr = t.ensureSandboxContainer(ctx)
	})
	if t.startErr != nil {
		return "", fmt.Errorf("failed to start sandbox container: %w", t.startErr)
	}

	p := BashToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}
	if err := validateShellCommand(p.Command); err != nil {
		return "", err
	}

	// Execute command in container via docker exec
	cmd := exec.CommandContext(ctx, "docker", "exec",
		"--workdir", t.workspaceDir,
		t.containerName,
		"sh", "-c", p.Command)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker exec failed: %w", err)
	}
	return string(output), nil
}

func (t *DockerBashTool) HostTool() Tool {
	if t.hostTool != nil {
		return t.hostTool
	}
	return t
}

func (t *DockerBashTool) ensureSandboxContainer(ctx context.Context) error {
	if t.workspaceDir == "" {
		return fmt.Errorf("workspace dir is required for docker sandbox")
	}
	if err := os.MkdirAll(t.workspaceDir, 0o755); err != nil {
		return err
	}
	if err := t.ensureSandboxImage(ctx); err != nil {
		return err
	}

	// First, try to start existing container
	startCmd := exec.CommandContext(ctx, "docker", "start", t.containerName)
	if startCmd.Run() == nil {
		// Container exists and started successfully
		return nil
	}

	// Container doesn't exist, create new one
	args := []string{"run", "-d",
		"--name", t.containerName,
		"--restart", "unless-stopped",
		"--network", "none",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "256",
		"--memory", "2g",
		"--cpus", "2",
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=256m",
		"--tmpfs", "/var/tmp:rw,nosuid,nodev,noexec,size=128m",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp",
		"-e", "PYTHONDONTWRITEBYTECODE=1",
		"-v", t.workspaceDir + ":/workspace:rw",
		"-v", t.workspaceDir + ":" + t.workspaceDir + ":rw",
		"-w", t.workspaceDir,
	}
	if t.pptMasterRoot != "" {
		args = append(args,
			"-v", t.pptMasterRoot+":"+t.pptMasterRoot+":ro",
			"-v", t.pptMasterRoot+":/workspace/ppt-master:ro",
		)
	}
	args = append(args, t.image, "sleep", "infinity")
	createCmd := exec.CommandContext(ctx, "docker", args...)

	output, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create sandbox container: %s: %w", string(output), err)
	}
	return nil
}

func (t *DockerBashTool) ensureSandboxImage(ctx context.Context) error {
	inspectCmd := exec.CommandContext(ctx, "docker", "image", "inspect", t.image)
	if inspectCmd.Run() == nil {
		return nil
	}
	dockerfile := resolveSandboxDockerfile()
	if dockerfile == "" {
		return fmt.Errorf("sandbox image %q not found and sandbox.Dockerfile is unavailable", t.image)
	}
	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", t.image, "-f", dockerfile, filepath.Dir(dockerfile))
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build sandbox image %q: %s: %w", t.image, string(output), err)
	}
	return nil
}

func resolveSandboxDockerfile() string {
	candidates := []string{
		filepath.Join("tool", "sandbox.Dockerfile"),
		filepath.Join("background", "tool", "sandbox.Dockerfile"),
		filepath.Join(filepath.Dir(os.Args[0]), "tool", "sandbox.Dockerfile"),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "tool", "sandbox.Dockerfile"),
			filepath.Join(cwd, "background", "tool", "sandbox.Dockerfile"),
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
