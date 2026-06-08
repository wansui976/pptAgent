package main

import (
	"os"

	"github.com/joho/godotenv"

	"lingxi/background/agent"
	"lingxi/background/server"
	"lingxi/background/shared"
	"lingxi/background/shared/log"
	"lingxi/background/tool"
)

func main() {
	_ = godotenv.Load()

	appConf, err := shared.LoadAppConfig("config.json")
	if err != nil {
		log.Errorf("Failed to load config.json: %v", err)
		panic(err)
	}

	db, err := server.InitDB("ch10.db")
	if err != nil {
		log.Errorf("Failed to initialize database: %v", err)
		panic(err)
	}

	paths := server.ResolvePaths(appConf.Paths)

	tools := []tool.Tool{
		tool.CreateBashToolWithPPTMaster(paths.WorkspaceRoot, paths.PPTMasterRoot),
		tool.NewReadToolWithRoots(paths.WorkspaceRoot, paths.PPTMasterRoot),
		tool.NewWriteToolWithRoot(paths.WorkspaceRoot),
		tool.NewEditToolWithRoot(paths.WorkspaceRoot),
		tool.NewLoadSkillToolWithRoot(paths.PPTMasterRoot),
		tool.NewListPPTTemplatesToolWithRoot(paths.PPTMasterRoot),
	}
	// web_search 仅在 config.json 配置了 qwen_search.api_key 时注册；
	// 未配置时新流水线 research 阶段会因缺工具而退回 StageNone（不阻塞旧流程）。
	qwenClient := shared.NewQwenSearchClient(appConf.LLMProviders.QwenSearch)
	if qwenClient.Configured() {
		tools = append(tools, tool.NewWebSearchTool(qwenClient))
	}

	a := agent.NewAgent(appConf.LLMProviders.FrontModel, agent.BuildSystemPrompt(paths.WorkspaceRoot), paths.WorkspaceRoot, tools)
	a.SetPipelineV2(appConf.PPTPipelineV2 && qwenClient.Configured())
	s := server.NewServer(db, a, paths)
	router := server.NewRouter(s)

	addr := os.Getenv("LINGXI_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if err := router.Run(addr); err != nil {
		log.Errorf("Server failed: %v", err)
		panic(err)
	}
}
