package tool

import "testing"

func TestCreateBashTool_DefaultsToDockerBash(t *testing.T) {
	t.Setenv("LINGXI_USE_DOCKER_BASH", "")
	t.Setenv("LINGXI_ALLOW_HOST_BASH", "")

	toolInstance := CreateBashTool("/tmp/demo-workspace")
	if _, ok := toolInstance.(*DockerBashTool); !ok {
		t.Fatalf("CreateBashTool() should return *DockerBashTool by default, got %T", toolInstance)
	}
}

func TestCreateBashTool_CanExplicitlyAllowHostBash(t *testing.T) {
	t.Setenv("LINGXI_ALLOW_HOST_BASH", "1")

	toolInstance := CreateBashTool("/tmp/demo-workspace")
	if _, ok := toolInstance.(*BashTool); !ok {
		t.Fatalf("CreateBashTool() should return *BashTool when host bash is explicitly allowed, got %T", toolInstance)
	}
}
