# 开发维护说明

本说明仅面向维护者的开发环境。公开安装方法见仓库根目录 README。

- `go run .` 启动本地 HTTP 服务，默认读取工作目录下的 `config.yaml` 与 `ocs-tiku.db`，并提供 `frontend/dist/` 页面。可用 `-data-dir`、`-frontend-dir` 和 `-listen` 单独指定测试目录及监听地址。
- 首次运行生成默认配置；已有 Python 版本的 YAML 与 SQLite 表直接沿用。`OCS_TEXT_API_KEY`、`OCS_TEXT_BASE_URL`、`OCS_TEXT_MODEL`、`OCS_VISION_API_KEY`、`OCS_VISION_BASE_URL`、`OCS_VISION_MODEL`、`OCS_PORT` 在运行时覆盖文件配置。
- `cd frontend && npm ci && npm run build && npm run lint` 检查前端。开发时 `npm run dev`，Vite 将 `/api` 和 `/images` 转发至 `127.0.0.1:8000`。
- `go test . && go vet .` 检查 Go 后端；测试使用临时配置、临时数据库和本地模拟模型，不调用实际供应商。
- Python 服务与旧 HTML 模板放在 `legacy/python/`，仅保留历史参考。它们不参与新服务运行或打包。
- Go 服务当前没有 Python 版 PyInstaller 打包脚本；发布二进制时应先构建前端，再为目标平台编译 Go 程序，并将 `frontend/dist/` 放在相对于工作目录的同名路径下。

提交前检查 `.gitignore`、`git status --short`、`git diff --cached --name-only` 与暂存内容。不要提交真实密钥、`.env`、`config.yaml`、数据库、日志和下载的题目图片。此仓库的维护者专用网络环境和本地绝对路径不适用于公开安装说明。
