# 给当前连接授权 GitHub 仓库写入

目标仅限 `HiChen85/pal--reverse--learn`。公开仓库可读不等于当前连接可写。本地 git push 与 GitHub 连接器 Create Blob 都曾返回 HTTP 403；账号对仓库有管理员权限也不代表连接令牌具有同样权限。

1. ChatGPT：头像 → 设置 Settings → 应用 Apps（某些界面称 Apps & Connectors）→ GitHub。
2. 选择连接、重新连接或管理连接。若已连接却没有补充授权入口，可断开 GitHub 后重连；不需要移除其他应用。
3. 在跳转的 GitHub 页面确认当前账号是 `HiChen85`，安装目标选择个人账号 `HiChen85`。
4. 如果显示仓库选择，选择 Only select repositories，仅添加 `pal--reverse--learn`，按页面点击 Install / Authorize / Save。
5. 打开 https://github.com/settings/installations ，找到名称/发布者与刚才授权页面一致的应用 → Configure，确认仓库已纳入授权。不要凭名称把不相关应用也授权。
6. 若显示 Review permissions / Accept new permissions，请检查请求范围：上传代码需要 Contents 的 Read and write；修改 GitHub Actions 工作流还可能需要 Workflows 写入。如果只想上传源码，可以先不授权工作流权限。
7. 返回 ChatGPT，必要时重新连接，让连接使用更新的授权，然后告诉 agent 重试。

如果 GitHub 授权页只请求读取权限，通常无法在 Configure 页手动将其改成读写：GitHub App 权限由应用声明。需要选择产品提供的可写连接方式。ChatGPT 的“自动允许操作”等审批设置也不会增加 GitHub 的服务端权限。

如果看到的只有 Authorized OAuth Apps 而不是 Installed GitHub Apps，也不要自行创建或粘贴个人访问令牌；可从 ChatGPT 的 GitHub 连接入口重新发起授权，确认该入口是否支持仓库写入。当前工具不能代替用户完成 GitHub 登录、安装批准或权限升级。

没有写入权限时，可以先下载源码压缩包进行本地开发；源码与 review 记录不会因此丢失。请勿在对话中发送密码或 token。
