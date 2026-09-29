# OCS-Tiku 开发维护约定

本文件面向本项目的开发维护环境。项目独立仓库为 https://github.com/onprs/OCS-Tiku.git，默认开发分支为 `main`；`D:/Code/OCS` 的上层 Git 仓库不是本项目的推送目标。维护环境的目录和网络设置不应作为公开用户的安装前提。

## 项目结构

- `main.py` 启动本地 Uvicorn 服务并打开浏览器；`server.py` 提供 `/api/search`、静态资源及前端页面，`dashboard.py` 提供记录、统计、配置和模型测试接口。
- `solver.py` 组织直接作答与分步作答；`preprocessor.py` 下载并识别题目图片；`models/` 定义文本和视觉模型适配器及注册表。
- `config.py` 读取配置、环境变量覆盖，并在缺少 `config.yaml` 时生成默认配置；设置接口也会写入该文件。`store.py` 使用 SQLite 存储答题记录，`db.py` 为另一个数据库实现，修改存储逻辑时先确认实际调用入口。
- `frontend/` 是 React、TypeScript、Vite 应用；生产构建输出到 `frontend/dist/`，由后端提供页面。`templates/` 和 `static/` 为仓库中保留的旧页面资源，修改页面前先确认当前入口。
- `build.spec`、`build.bat` 用于 Windows 上的 PyInstaller 打包，打包前先构建前端。

## 修改与验证

- 后端依赖见 `requirements.txt`，前端依赖见 `frontend/package-lock.json`。在项目根目录安装 Python 依赖、进入 `frontend/` 执行 `npm ci` 和 `npm run build` 后，运行 `python main.py`；默认监听 `127.0.0.1:8000`。
- Python 代码至少执行 `python -m compileall -q config.py dashboard.py db.py main.py preprocessor.py server.py solver.py store.py models`；前端改动执行 `npm run build` 和 `npm run lint`。现有 lint 结果必须如实记录，失败时不要宣称检查通过；涉及行为改动时补充相应测试。
- 不将真实 API Key、`.env`、`config.yaml`、数据库、日志、下载图片、证书或本机调试产物提交到 Git。提交前检查 `.gitignore`、`git status --short`、`git diff --cached --name-only` 和暂存内容；示例配置只能使用占位值。

## GitHub 同步

- **每完成一批修改，都要在本项目独立仓库完成验证、核对暂存文件、提交，并推送到 GitHub 的 `origin/main`**；不要只保留本地提交，也不要把上层仓库的变更混入本项目。
- 推送前先核对远端分支和本地状态；远端出现新提交时先同步并处理冲突，不强制推送。推送失败须保留本地提交，记录失败原因与未同步状态，不得声称已经同步。
