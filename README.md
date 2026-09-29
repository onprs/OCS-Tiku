# OCS 题库

本地运行的 OCS 大模型题库服务。接收题目与选项，调用已配置的模型返回答案，并在网页中查看请求记录、统计和模型设置。支持文字题目、图片识别及视觉模型直接答题。

## 安装与启动

需要 Git、Go 1.25 或更高版本、Node.js 22.12 或更高版本及 npm。

```sh
git clone https://github.com/onprs/OCS-Tiku.git
cd OCS-Tiku
cd frontend
npm ci
npm run build
cd ..
go run .
```

在浏览器中打开 http://127.0.0.1:8000 。首次启动会在当前目录创建 `config.yaml` 和 `ocs-tiku.db`。在「引擎设置」中填写自己的模型 API 地址、名称及密钥，并使用「测试连通性」检查配置；图片题目需设置视觉模型。

在「数据看板」复制 OCS 题库配置，粘贴到 OCS 脚本的题库配置中。服务默认仅监听本机，使用期间保持终端运行；按 Ctrl+C 停止。已有旧版 `config.yaml` 与 `ocs-tiku.db` 可继续放在项目根目录使用。
