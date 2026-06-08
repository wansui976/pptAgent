# 灵犀前端

当前前端已经切到 Go 后端驱动模式，不再保留百炼 API Key 输入框和本地 mock 对话。

## 当前能力

- 会话列表来自 `GET /api/conversation?user_id=...`
- 发送消息走 `POST /api/conversation/:id/message`
- 流式响应通过 `text/event-stream` 解析 `content / tool_call / tool_result / ppt_*`
- 工具调用和工具结果在 AI 消息里以可折叠面板展示
- PPT 生成过程会在右侧面板实时展示项目创建、SVG 页面和导出下载按钮

## 启动方式

前端是静态页面，直接打开 [index.html](/workspace/front/index.html) 即可；更推荐通过本地静态服务器访问，这样 `window.location.origin` 会稳定指向当前域名。

示例：

```bash
cd /workspace/front
python3 -m http.server 3000
```

然后访问：

- [http://localhost:3000](http://localhost:3000)

## 联调前提

- Go 后端运行在 `http://localhost:8080`
- 后端需要开启 `/api/conversation*` 和 `/api/projects/:project_name/exports/:file`
- 若前端和后端不在同源下运行，需要后端补充 CORS 配置

## 备注

- 上传文件目前仍在前端侧做基础解析，解析结果会被拼进发送给后端的 `query`
- 刷新页面后会恢复会话历史；PPT 侧栏当前优先恢复下载能力，SVG 历史重建仍是轻量实现
