---
name: pr
description: "创建 Pull Request。用法：/pr。当用户说 /pr、'提 pr'、'创建 pr'、'提交 pr'、'create pr'、'open pr' 时触发。自动在当前分支 commit 并 push，若当前在 master/main 则创建新分支，最后用 gh 命令提交 PR。"
---

# Create Pull Request

## 流程

### 1. 检查当前分支

运行 `git branch --show-current` 获取当前分支名。

**如果在 master 或 main 分支：**
需要先创建新分支。根据 `git diff HEAD` 的改动内容，自动生成分支名：

- **类型前缀**：根据改动性质确定 — `feat`（新功能）、`fix`（修复）、`chore`（杂项）、`refactor`（重构）
- **日期**：当前日期，格式 `YYYYMMDD`
- **简短描述**：基于改动内容生成 2-3 个英文词，用 `-` 连接

格式：`<type>/<YYYYMMDD>-<short-desc>`

例如：
- 新增了用户登录功能 → `feat/20260601-user-login`
- 修复了支付回调的 bug → `fix/20260601-payment-callback`
- 更新了依赖版本 → `chore/20260601-update-deps`

生成分支名后执行 `git checkout -b <branch-name>` 创建并切换到新分支。

**如果已在其他分支：** 直接使用当前分支，跳过此步骤。

### 2. 检查改动

运行 `git status` 和 `git diff HEAD` 查看所有改动的文件和内容。

- 如果没有任何改动（包括未暂存和未提交的），提示用户后退出
- 执行 `git add .` 添加所有改动文件

### 3. 生成 commit message

阅读 diff 内容，用中文简洁总结改动（1-2 行）。风格参考项目历史提交记录。

### 4. 执行 commit

运行 `git commit -m "commit message"` 提交。

- **不要**在 commit message 中添加 `Co-Authored-By`、`Reviewed-by` 或任何其他署名信息
- 只保留纯中文的 commit message

### 5. 处理 pre-commit 失败

如果 commit 被 pre-commit hook 拦截：

- **只有**错误信息涉及 `ruff`（Python 格式/lint）或 `prettier`（前端格式）时，才自动重试
- 自动修复后，重新 `git add .` 再执行 `git commit`
- 如果第二次仍然失败，把错误信息展示给用户，让用户决定
- 如果是其他类型的 hook 错误（如测试失败、自定义 hook），直接展示错误给用户，不自动重试

### 6. Push

commit 成功后，执行 `git push -u origin <当前分支名>` 推送到远程。

如果 push 失败，展示错误信息后退出。

### 7. 创建 PR

push 成功后，用 commit message 的第一行作为 PR 标题，body 留空：

```bash
gh pr create --title "<commit message 第一行>" --body ""
```

- 创建成功后，展示 PR 的 URL
- 如果 `gh pr create` 失败，展示错误信息

### 8. 切回 master

PR 创建成功后，用 `AskUserQuestion` 询问用户是否切回 master 分支。

- 如果用户选择"是"，执行 `git checkout master`
- 如果用户选择"否"，不做操作
