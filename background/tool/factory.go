package tool

import (
	"log"
	"os"
	"os/exec"
)

// checkDockerAvailable checks if docker command is available
func checkDockerAvailable() bool {
	// Use `docker ps` to check if docker daemon is running
	// `docker ps` will fail if docker daemon is not running
	cmd := exec.Command("docker", "ps")
	return cmd.Run() == nil
}

func shouldUseDockerBash() bool {
	if allowHostBash() {
		return false
	}
	value := os.Getenv("LINGXI_USE_DOCKER_BASH")
	if value == "" {
		return true
	}
	return value == "1" || value == "true" || value == "TRUE"
}

func allowHostBash() bool {
	value := os.Getenv("LINGXI_ALLOW_HOST_BASH")
	return value == "1" || value == "true" || value == "TRUE"
}

// CreateBashTool creates a bash tool.
// Default behavior is host bash. Docker is only enabled with explicit opt-in.
func CreateBashTool(workspaceDir string) Tool {
	return CreateBashToolWithPPTMaster(workspaceDir, "")
}

func CreateBashToolWithPPTMaster(workspaceDir string, pptMasterRoot string) Tool {
	if !shouldUseDockerBash() {
		log.Printf("Docker bash disabled by LINGXI_ALLOW_HOST_BASH, using host bash tool")
		return NewBashToolWithRoot(pptMasterRoot)
	}
	if workspaceDir == "" {
		log.Printf("Docker bash requested but workspace dir is empty")
		return NewDockerBashToolWithPPTMaster("", workspaceDir, pptMasterRoot)
	}
	containerName := generateContainerName(workspaceDir)
	log.Printf("Docker bash enabled, using DockerBashTool with sandbox container '%s'", containerName)
	return NewDockerBashToolWithPPTMaster("", workspaceDir, pptMasterRoot)
}
