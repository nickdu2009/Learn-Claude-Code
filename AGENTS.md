# AGENTS.md

本文件用于指导（人类与 AI）在本仓库中高效、可控地完成工程任务，尽量保持每个 Session 的教学目标清晰、实现最小化且可复现。

## 项目概览

- **语言/版本**：Go 1.22+
- **目标**：以 Go 复刻 `learn-claude-code` 的 12 个渐进式 Session（见 `README.md`）
- **模型与 API**：通义千问（Qwen），OpenAI Compatible API（见 `.env.example` 与 `README.md`）

## 目录结构（约束）

- **`agents/sXX_*`**：每个 Session 一个独立目录，包含该 Session 的 `main.go`（以及必要的测试文件）
- **`pkg/`**：跨 Session 复用的公共能力（例如 client、tools、loop、recorder 等）
- **`scripts/`**：本地开发辅助脚本（例如 devtools viewer）
- **`.devtools/`**：本地追踪输出（可能包含明文 prompt 与工具输出）
- **`.local/`**：本地/测试产物目录（必须被忽略，禁止提交）

原则：
- **保持最小化**：每个 Session 只引入“该 Session 要讲”的一个机制，避免偷跑后续概念。
- **复用公共包**：能复用的放 `pkg/`，但不要为了“抽象”而抽象；以教学可读性优先。

## 常用命令

### 初始化与运行

```bash
go mod tidy
cp .env.example .env
go run ./agents/s01_agent_loop/
```

### 质量与测试（提交前）

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./...
```

### 本地 DevTools Viewer（可选）

```bash
./scripts/devtools-viewer.sh
```

启用记录（`.env`）：

```bash
AI_SDK_DEVTOOLS=1
AI_SDK_DEVTOOLS_PORT=4983
```

## 变更策略（对 AI 特别重要）

- **优先改“局部”而非“全局”**：除非该变更确实要影响多个 Session，否则把改动限制在一个 `agents/sXX_*` 目录内。
- **API 兼容性**：公共包（`pkg/`）的变更要避免破坏既有 Session；如必须破坏，请同步修复受影响的 Session 与测试。
- **错误处理**：所有错误必须显式处理；禁止忽略关键错误（例如 I/O、网络请求、JSON 编解码）。
- **命名与风格**：
  - 标识符使用英文、语义清晰（Clean Code / DRY / SOLID）
  - 标准库相关注释用英文；复杂逻辑说明用中文（避免“描述代码在做什么”的无意义注释）

## 安全与隐私（必须遵守）

- **不要提交敏感信息**：`.env`、API Key、token、私有 endpoint、真实用户数据等。
- **注意明文落盘**：DevTools 追踪数据（默认写入仓库根目录下的 `.devtools/generations.json`，也可通过 `AI_SDK_DEVTOOLS_DIR` 指定）可能包含 prompts / 工具参数 / 工具输出等明文内容，仅建议本地使用；如需在 CI/共享环境使用，先评估脱敏与访问控制。
- **测试数据专用目录**：测试运行中 test case 产生的任何落盘数据必须写入 `.local/`（例如 `.local/test-artifacts/`），且 `.local/` 必须在 `.gitignore` 中被忽略，禁止提交。
- **测试写文件的硬性规则**：
  - **禁止写入源码目录**：测试（以及被测试的 Agent/Tools）不得在 `agents/`、`pkg/`、仓库根目录等源码区域创建/覆盖文件（包括误写 `hello.txt` 之类的产物）。
  - **统一写入路径**：凡是测试中会“写文件/建目录”的行为（例如 `write_file`、`bash` 创建项目文件、生成中间产物），工作目录必须指向仓库根目录下的 `.local/test-artifacts/<session>/<real|fake>/<testname>/<run-id>/`。
    - `run-id` 必须能区分每次执行（推荐时间戳），以便**保留每次集成测试的完整产物**用于回放与对比。
  - **禁止自动清理产物**：集成测试默认**不要**用 `t.Cleanup` 删除 `.local/test-artifacts/...` 目录；需要清理时由开发者手动删除（或另写专门的清理脚本/命令）。
  - **DevTools 与测试产物分离**：DevTools 追踪文件只允许写入仓库根目录下的 `.devtools/`（测试中可在 `TestMain` 设置 `AI_SDK_DEVTOOLS_DIR=<repo>/.devtools`），**不得写入 `.local/`**。
- **真实测试用例（Prompt Fixture）规范**：
  - **以 Markdown 存储**：真实/通用测试 case 的需求文本应保存为 `pkg/testcases/*.md`。
  - **以 embed 读取**：测试与代码读取这类 fixture 时优先使用 `go:embed`，避免依赖 repo root 搜索或运行时文件路径导致的不稳定。
- **最小权限原则**：新增外部依赖或脚本前，优先评估安全影响与维护成本。

## 新增/修改 Session 的 checklist

- [ ] 目录为 `agents/sXX_name/`，且入口为 `main.go`
- [ ] 只引入本 Session 需要的新机制（不提前引入后续机制）
- [ ] 能复用的逻辑放入 `pkg/`，但保持教学可读性
- [ ] `gofmt -w .` 后无格式问题
- [ ] `go test ./...`、`go vet ./...`、`go build ./...` 通过
- [ ] `README.md` 如有新增功能/命令差异需同步更新

