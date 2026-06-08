# 灵犀后端

当前后端负责提供会话 CRUD、SSE 流式对话、工具调用转发，以及 PPT 相关事件与导出下载。

## 当前接口

- `POST /api/conversation`
- `GET /api/conversation?user_id=...`
- `PATCH /api/conversation/:conversation_id`
- `DELETE /api/conversation/:conversation_id`
- `GET /api/conversation/:conversation_id/message`
- `POST /api/conversation/:conversation_id/message`
- `GET /api/projects/:project_name/exports/:file`

## 当前工具集

- `bash`
- `read`
- `write`
- `edit`
- `load_skill`

其中：

- `load_skill` 会读取 `ppt-master/skills/ppt-master/SKILL.md`
- `write` 会在写入 `svg_output/*.svg` 后追加 `ppt_page_svg` 事件
- `bash` 在识别到 `project_manager.py init` 和 `svg_to_pptx.py` 后会追加 PPT 项目创建与导出事件

## SSE 事件

基础事件：

- `content`
- `reasoning`
- `tool_call`
- `tool_result`
- `error`

PPT 扩展事件：

- `ppt_project_created`
- `ppt_page_svg`
- `ppt_exported`

## 运行方式

当前后端已经整理为独立 Go module，可直接在 [background](/workspace/background) 目录执行：

```bash
go test ./...
go build -o /tmp/lingxi-background ./main
```

如果要真正启动服务，还需要准备：

- `config.json`
- 对应模型的 `base_url / api_key / model`
- 前端或反向代理访问 `:8080`

## 代码现状

为了先让 Web 主链可编译、可测试，我把旧教程遗留的实验性目录临时挪到了：

- [background/_disabled](/workspace/background/_disabled)

这些目录目前不参与 Web 服务主链，也不会影响当前会话 API、SSE、PPT 事件和导出功能。

## PPT 沙箱

推荐使用 [sandbox.Dockerfile](/workspace/background/tool/sandbox.Dockerfile) 构建容器镜像，预装：

- `python3`
- `pandoc`
- `python-pptx`
- `lxml`
- `Pillow`
- `cairosvg`

当前 `CreateBashTool("/workspace/workspace")` 已经预留了 Docker 沙箱入口。
