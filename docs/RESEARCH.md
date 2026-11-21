# PAL1 结构研究与证据边界

## 结论

公开的 SDLPAL 源码足以确认 MKF 资源目录的基本布局，以及对象、脚本、角色与战斗模块之间的关系。这支持实现只读目录检查和独立的扩展内容架构；不能据此声称已逆向验证原版 PAL1 可执行文件、完全兼容原版存档、复现全部 opcode 或复刻原版战斗。

本轮对象是公开重实现 **sdlpal/sdlpal**，固定提交 `d4bf69b64d335cd680555af9df90e7ec4f3006a3`（本地 `/workspace/reference/sdlpal`）。方法为本地源码静态阅读，使用 git、rg、文本读取和 Go 合成数据测试。当前会话没有实际 REA MCP/CLI 分析工具，未调用反汇编器、反编译器、二进制追踪或目标程序。本报告采用 REA 的观察/推断/未知分离方式，不称作 REA 已执行的分析。未获取或运行原版商业程序、资源文件；没有原版版本号、哈希或执行证据。

## 可追溯的源码观察

以下置信度只描述“该 SDLPAL 提交如何实现”，不扩展成所有 PAL1 发行版的事实。表内块索引均从 0 开始。

| 证据 ID | 直接观察（高置信度） | 固定源码位置 |
| --- | --- | --- |
| R01 | MKF 首个 32 位小端值经 `(值-4)/4` 得到块数；相邻两个小端偏移相减是块长度；读块从前一偏移开始。 | [palcommon.c:855–1012](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/palcommon.c#L855-L1012) |
| R02 | DOS 对象联合体容纳 6 个 WORD，另一对象联合体容纳 7 个 WORD；物品保存图像、价格、使用/装备/投掷脚本及 flags；后者多描述脚本槽。 | [global.h:144–165](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L144-L165)、[global.h:219–243](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L219-L243) |
| R03 | SCRIPTENTRY 有一个 WORD opcode 和三个 WORD operand；脚本表从 SSS.MKF 块 4 读取。 | [global.h:239–243](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L239-L243)、[global.c:289–290](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L289-L290) |
| R04 | 默认事件/场景/对象分别来自 SSS.MKF 块 0/1/2。DOS 对象读入后转成较宽布局并移动 flags；角色表来自 DATA.MKF 块 3。 | [global.c:401–429](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L401-L429) |
| R05 | PLAYERROLES 是按属性组织的角色数组，包含头像、战斗/地图精灵、姓名索引、等级、最大/当前 HP/MP、装备、攻防、敏捷、抗性、技能和音效等。不是仅有姓名与 HP 的记录。 | [global.h:297–336](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L297-L336) |
| R06 | DATA.MKF 块 0/1/2/4/5 分别加载商店、敌人、敌队、法术、战场表。注意全局法术加载用块 4，而 global.h 的某处注释写块 3；应以代码路径为本轮直接证据，真实文件仍需验证。 | [global.c:292–298](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L292-L298)、[global.h:176–178](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L176-L178) |
| R07 | TriggerScript 解释循环按 opcode 分发，支持停止、跳转、子脚本调用；opcode 0x0007 调用 PAL_StartBattle，并依据败北/逃跑结果选择后续脚本地址。 | [script.c:3197–3266](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3197-L3266)、[script.c:3316–3335](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3316-L3335) |
| R08 | 战斗入口初始化状态后运行 PAL_BattleMain，胜利时单独调用奖励结算；frame、行动验证、玩家执行、敌人执行位于独立函数。 | [battle.c:1792–1819](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/battle.c#L1792-L1819)、[fight.c:1073](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L1073)、[fight.c:3249](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L3249)、[fight.c:3577](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L3577)、[fight.c:4551](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L4551) |
| R09 | PAL_CLASSIC 编译分支构建行动队列，速度带随机扰动且处理双动；非经典分支按计时条推进。基础伤害另有分段公式。不能将任意简化回合公式称为此引擎的等价重写。 | [fight.c:1096–1100](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L1096-L1100)、[fight.c:1319–1324](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L1319-L1324)、[fight.c:1440–1492](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L1440-L1492)、[fight.c:131–170](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/fight.c#L131-L170) |
| R10 | 版本检测综合多个资源首个非空块的 YJ_1 签名与对象表长度，源码明确承认签名判断非百分之百正确。不能凭单个 MKF 目录给出确定 DOS/Windows 判定。 | [global.c:50–101](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L50-L101) |

## 对本项目的设计推断

- 根据 R01，`internal/pal` 采用 32 位小端偏移目录；主动增加边界、单调性、块数和读块内存限制。这些限制是本工具的防御性设计，不是声称原版所有发行文件都满足的额外格式规则。
- 根据 R02–R07，原版物品效果和剧情需要执行脚本；首版的 JSON 物品 `HealHP/RestoreMP`、剧情图和 flags 是独立扩展协议，不能直接回写 SSS.MKF，也不能完整表达原版 opcode。
- 根据 R05，未来导入层应保留原始槽位、版本和未识别字段；当前工作台使用稳定字符串 ID，避免新增人物/物品与原版槽位冲突。
- 根据 R08–R09，把 Go 战斗模型与 UI/剧情分开有助于确定性测试。首版战斗规则独立设计；原版准确复现需明确经典/计时模式、伤害精度、随机数、状态、法术、道具脚本与奖励结算。
- 追加章节可以用命名空间和显式入口接入；与原版终章直接相连仍须验证目标版本的终章脚本和存档状态，本轮未完成。

## 已实现的 MKF 检查工具范围

`pal.Open(io.ReaderAt, size)` 仅读取目录，不解压，不执行内容；支持空块、零块目录和重复偏移。目录最多 `1<<20` 块，文件尺寸限制在 4–4294967295 字节，每次 `ReadChunk` 最大 64 MiB。`ChunkSize` 可查看更大块而不加载。目录末端之后的文件尾随字节允许存在，但不计入任一块。调用者持有 reader 并负责关闭，检查期间不能修改文件。

合成测试覆盖普通/空块、空目录、越界/负索引、截断目录、非对齐目录、逆序偏移、文件外偏移、目录规模上限、超大读块阻止、短读和切片独立性；另提供目录 fuzz 入口。合成测试通过不代表已测试真实游戏资源。

## 许可与素材边界

SDLPAL 的 README 声明 GPLv3，且明确说明不含 SoftStar 的原始商业代码和资源，建议购买原版取得数据：[README.md:28–45](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/README.md#L28-L45)。本轮未将 C 实现文件或游戏素材加入项目；Go 目录解析器根据格式观察编写，不作“clean-room 逆向”的声明。若后续移植/复制其代码，应保留对应版权、GPL 声明和来源，按项目所选兼容许可发布。网上能下载、非商用用途本身不能证明原版二进制和素材允许再分发。

## 未知项与最小后续步骤

1. 确定用户合法持有/获授权的具体 PAL1 版本，记录文件清单、SHA-256、平台和版本。不从不明资源站自动获取原版二进制。
2. 以该版本的 MKF 目录和已知小样本验证 R01–R06，再单独实现有输出上限的 YJ1/YJ2 解码与只读对象导入。
3. 连接实际 REA MCP 或提供本地 REA 输出，再根据指定可执行文件开展静态分析；运行前明确目标和生命周期。未连接工具前继续标注为源码研究。
4. 建立合法的最小 golden fixtures：脚本指令、角色对象、单次物品效果和一场固定输入战斗。记录期望结果及采集方法后，逐项对照 Go 实现，不能以源码相似推断运行一致。

## 本轮验证记录

Go 1.27.3，Linux amd64，2026-11-05：`go test ./internal/pal` 通过；`go vet ./internal/pal` 通过；`go test ./internal/pal -run '^$' -fuzz FuzzOpen -fuzztime=2s` 通过（约 44,973 次执行，2 个 worker）。这些是本项目合成输入的工具验证，不是 PAL1 运行取证。
