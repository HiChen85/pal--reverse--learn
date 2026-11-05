# PAL1 Go Studio · 故事工坊

`pal--reverse--learn` · learn rea

基于 PAL1 公开重实现资料开展学习，使用 **Go 1.27.3 + MyGo v0.7.4 原生 UI** 的剧情、人物、物品编辑器和可试玩的纯 Go 战斗原型。

**当前是第一阶段可运行原型，不是已完成的仙剑一复刻。** 包含原创章节、独立扩展包、剧情分支、人物/物品编辑、攻击/防御/技能/物品战斗、进度存取及只读 MKF 目录检查；尚不支持读取原版人物/物品表、原版 opcode VM、地图探索、原版素材渲染和原版战斗算法。研究内容与后续路线分别见 [研究证据](docs/RESEARCH.md) 和 [实现方案](docs/PLAN.md)。

本轮未连接实际 REA MCP/分析引擎，因此报告是可追溯的 SDLPAL 源码研究，未声称 REA 已反编译原版二进制。示例内容独立创作，不含原版程序、台词、美术或音频。

## 运行

安装 Go **1.27.3 或更高版本**，在仓库根目录运行：

```sh
go run ./cmd/palstudio
```

使用 MyGo 的原生 Go UI，无须 Node、Bun 或浏览器前端。桌面运行需要操作系统图形会话及 MyGo 的平台运行库；Linux 使用 GTK 3、Wayland/X11 和图形驱动，具体依赖以 [MyGo v0.7.4 文档](https://github.com/egoist/mygo/tree/v0.7.4/docs) 为准。无显示器时可运行：

```sh
go run ./cmd/paltool demo
go test ./...
```

构建桌面程序和命令行工具：

```sh
make build
# 或
go build -o bin/palstudio ./cmd/palstudio
go build -o bin/paltool ./cmd/paltool
```

本工作区已将 Go 工具链放在 `/workspace/toolchains/go`；若系统 `go` 不是 Go 语言编译器，可用 `/workspace/toolchains/go/bin/go`，并设置可写的 `GOPATH=/workspace/toolchains/gopath` 和 `GOCACHE=/tmp/pal-go-cache`。这些路径只用于当前开发环境，不是用户机器的安装要求。

## 编辑与试玩

1. 启动后载入原创示例《雨后余音》。在“剧情”中新增节点、对白及分支，在“人物”“物品”中新增记录或修改数值。
2. “校验并应用”检查所有 ID、跨引用、数值和分支关系。成功后重置试玩；失败会保留已应用内容和运行进度。
3. “保存内容包”保存当前合法草稿为 JSON。未修改内容时保存不会重置试玩。“完整 JSON”可编辑战斗遭遇、敌人及 flags；先从表单同步，修改后点击“从 JSON 载入草稿”，再校验应用。
4. 在“试玩”选择入口开始，选择对白分支，进入战斗。战斗必须胜利才能继续；落败后可重试。战斗计算完全由 `internal/battle` 的 Go 代码执行。
5. “保存进度”与“读取进度”支持战斗中途恢复。存档保存内容包 SHA-256 和确定性行动序列；内容编辑后旧进度会被拒绝，避免错误套用状态。

试玩规则：人物列表前 **最多三位**组成队伍；每种物品初始三份，每次遭遇恢复全员 HP/MP；获胜后物品余量延续，失败重试还原本场开始时的物品。没有经验、装备、状态异常或奖励系统。以上是扩展原型规则，**并非原版 PAL1 数值行为**。技能固定消耗 3 MP，攻击力加倍；伤害为 `max(1, attack-defense/2)`，防御将伤害减半。敌人按速度顺序自动行动。

## 追加后续章节

```sh
go run ./cmd/paltool export-demo chapter.json
go run ./cmd/paltool merge chapter.json examples/chapter-two.json combined.json
go run ./cmd/paltool validate combined.json
```

也可在 UI 的扩展包路径中输入 `examples/chapter-two.json`，点击“追加章节”，保存合并内容包。扩展可以引用基础包人物和节点，不能覆盖同 ID 的基础记录，合并失败不修改工作区。独立扩展不一定能通过单独 `validate`，应通过与基础包合并进行校验。

合并保留基础包默认入口；第二章入口是 `river-arrival`。在试玩下拉选择该入口可独立预览。若要保持前章 flags 连贯进入后续故事，在剧情编辑器中将第一章末节点 `ending` 的“后续节点”设为 `river-arrival` 并应用；从基础默认入口重新运行即可连续推进。独立“从入口开始”会重置 flags 与初始物品。

内容文件 `schema_version=1`，使用稳定字符串 ID；条件选项的 `require_flags` 与节点 `set_flags` 可在完整 JSON 编辑。缺失 flag 视为 false，条件全部匹配才显示选项。允许作者设计循环；所有选择都被条件隐藏时需要修改内容，不能强制跳过。

## 命令行与资源研究

```sh
go run ./cmd/paltool play chapter.json
go run ./cmd/paltool mkf /path/to/your/data.mkf
```

`play` 支持交互式分支选择、战斗和进度存取，用 `help` 查看命令。MKF 工具只列目录和块长度，不解压、执行或改写文件；已对目录规模、偏移和读取大小设限。测试使用合成数据，未对真实原版数据作兼容性承诺。

## Windows 98 柔情篇全流程适配进度

已确认后续目标为 Windows 98 柔情篇。新增 `palverify`：只读读取场景/事件/对象/脚本/对话、固定文件摘要、静态 opcode 审计和带显式选择输入的部分触发脚本记录。合成测试已通过，**尚未提供真实目标数据，原版主线和支线未验证**；等待、移动、自动脚本和支线状态仍需补齐，不能把扫描完成当成通关。

使用方法见 [palverify 工具](docs/PALVERIFY.md)，验收标准见 [主支线验证](docs/WIN98_VALIDATION.md)，待处理问题见 [BUGS](docs/BUGS.md)。GitHub 上传授权步骤见 [连接授权说明](docs/GITHUB_AUTH.md)。

## APK 数据源初筛

```sh
go run ./cmd/palverify apk -file /path/to/game.apk
```

只检查归档元数据、文件摘要和 PAL98 必需资源的文件名候选。Git LFS 指针需要先取得原始对象；文件名匹配不表示资源格式或完整剧情已经验证。

## 界面预览

以下为 MyGo 原生无窗口渲染截图；尚未实测真实桌面窗口。

![剧情编辑器](docs/screenshots/editor.png)

![纯 Go 战斗试玩](docs/screenshots/battle.png)

## 验证与开发

```sh
go test -race ./...
go vet ./...
```

测试涵盖严格内容解析、扩展合并与原子保存、非法战斗行动不改状态、确定性回合、剧情分支/胜利门控、战斗中途存档、错误存档不替换会话，以及 MyGo 无窗口真实控件交互。CI 配置了三平台构建；本地验证结果见 [review 记录](docs/REVIEW.md)。

分层：`content` 定义内容；`pal` 检查 MKF；`battle` 结算战斗；`story` 运行图；`studio` 管理编辑与存档；两个 `cmd` 提供原生 UI 和 CLI。

许可：保留仓库原有 MIT 许可，详见 [LICENSE](LICENSE) 和 [第三方说明](THIRD_PARTY_NOTICES.md)。原版游戏素材应从合法持有的版本在本地导入；本仓库不提供素材下载。
