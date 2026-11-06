# Windows 98 柔情篇：主线与支线对话验证

## 当前结论与版本身份

**尚未验证原版主线或支线可完整运行。** 当前已取得用户指定 APK 中的真实数据并固定摘要，实际采用 DOS 六 WORD 对象布局、Big5 与 YJ1；仍缺原 Windows 98 发行身份确认及原版行为基线。主线里程碑、支线条目总数、动态可达消息数与兼容通过率均不能由表计数推导。原创演示、合成回归、显式检查点和已完成的开场逻辑顺序回放分别只证明各自范围。

本阶段使用“Windows 98 柔情篇”作为用户给定的目标名称；SDLPAL 内部的 `fIsWIN95` 是其 Windows 数据布局/解压模式开关，**不能单凭这个变量名认定 Windows 95、98、简体、繁体或再发行版的数据完全相同**。目标身份以用户合法提供的数据清单、SHA-256、语言/发行介质和实际程序版本为准，不能由网上压缩包标题推断。已静态检查新 APK 并在本地提取数据，未安装或运行 APK/原 exe，也未运行 REA 工具。下面 W 编号是公开 SDLPAL 源码观察；样本实测单列于 [INPUT_DATA.md](INPUT_DATA.md)，两类证据不能互相冒充。

源码基准固定为 `sdlpal/sdlpal@d4bf69b64d335cd680555af9df90e7ec4f3006a3`；以下 W 编号均是此重实现的观察证据，不是原版执行取证。

## 数据与文本索引事实

| 证据 | 直接观察 | 固定源码 |
| --- | --- | --- |
| W01 | `WORD.DAT` 按 `dwWordLength` 字节定长分词，去末尾 ASCII 空格后按代码页转换；中国版本默认宽度为 10，但可配置。不能按 UTF-8 行文本读取，也不能按 10 个 Unicode 字符截断。 | [text.c:718–787](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L718-L787)、[palcfg.c:570](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/palcfg.c#L570) |
| W02 | SSS.MKF 块 3 是小端 DWORD 消息偏移表，条目数减 1 得消息数；消息 i 的原始字节来自 M.MSG 的 `[offset[i], offset[i+1])`，再做代码页转换。 | [text.c:793–807](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L793-L807)、[text.c:837–864](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L837-L864) |
| W03 | PAL_GetWord/PAL_GetMsg 直接按索引访问；普通 TriggerScript 的 `0xFFFF` 用 operand 0 作为消息号。不要全局给消息号加减 1；启用自定义消息文件时存在独立映射逻辑。 | [text.c:965–1006](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L965-L1006)、[script.c:3440–3467](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3440-L3467) |
| W04 | 对话模式由 `0x003B–0x003E` 决定，含位置/头像/颜色等参数；首行以 `：`、U+2236 或 `:` 结尾时可能作为人物标题。`0xFFFF` 本身没有通用的“说话人物 ID”。头像、文本标题和触发 NPC 不应被强制合为一个身份。 | [script.c:3391–3427](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3391-L3427)、[text.c:1714–1746](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L1714-L1746) |
| W05 | `-`、`'`、`@` 等可改变颜色；`$` 设置文字速度，`~` 可延迟并结束文本，括号改变等待图标，反斜杠转义；超过四行会等待翻页。原始控制串不能直接当普通标点删掉。 | [text.c:1472–1577](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L1472-L1577)、[text.c:1649–1657](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/text.c#L1649-L1657) |
| W06 | Windows 模式选择 YJ2 解压；DOS 模式选择 YJ1。Windows 对象结构为 7 个 WORD，DOS 为 6 个，不能混用。解压长度头读取规则也有分支。 | [global.c:202](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L202)、[global.h:219–243](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L219-L243)、[palcommon.c:1063–1080](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/palcommon.c#L1063-L1080) |

当前样本已按 Big5 严格解码并按 DOS 布局导入；其他目标仍须独立确认。“Windows 版”不自动等于 GBK，“中文”也不自动等于 Big5。导入层应保留消息 ID、原始偏移/字节哈希和解码配置。编码或控制串无法解释时输出错误/待验证项，不能用替代字符悄悄算作成功。

## 场景、事件与支线状态

| 证据 | 直接观察与兼容影响 | 固定源码 |
| --- | --- | --- |
| W07 | 场景含地图、进入脚本、传送脚本、事件索引。事件含触发/自动脚本入口、位置、状态、触发方式、消失计时和两种脚本计时。场景 n 的事件 ID 从 `scene[n-1].index+1` 到 `scene[n].index`，依赖下一条场景记录的边界。 | [global.h:82–120](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L82-L120)、[play.c:79–85](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/play.c#L79-L85) |
| W08 | 进入场景执行脚本并把返回入口写回；若又切换场景则中断旧场景更新。自动触碰由事件状态和空间距离决定；手动调查用另一个范围/方向搜索规则。 | [play.c:56–75](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/play.c#L56-L75)、[play.c:107–162](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/play.c#L107-L162)、[play.c:445–499](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/play.c#L445-L499) |
| W09 | 触发脚本和自动脚本入口会随返回值改变。自动脚本通常每帧一条（跳转例外），仅对活动且未消失事件运行；TriggerScript 的等待 opcode `0x0009` 会继续调用游戏更新。把所有自动脚本一次跑完或把等待当 no-op，会改变事件顺序与可达性。 | [play.c:150–189](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/play.c#L150-L189)、[script.c:3345–3369](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3345-L3369)、[script.c:3516–3606](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3516-L3606) |
| W10 | `0x0024/0x0025` 重设自动/触发入口，`0x0059` 请求下帧切换场景，`0x006D` 改写场景进入/传送脚本。剧情进度不只是布尔 flag；只保存当前文本节点会丢失支线进度。 | [script.c:1137–1154](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L1137-L1154)、[script.c:1870–1884](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L1870-L1884)、[script.c:2065–2087](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2065-L2087) |
| W11 | 钱款/物品/装备/队伍均可影响分支：钱款不足跳转、物品增减、背包物品数检查、装备数检查、队伍成员检查。不同操作使用的“数量”范围不完全相同，不能统一替换成一个布尔“已拥有”。 | [script.c:952–1023](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L952-L1023)、[script.c:1859–1867](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L1859-L1867)、[script.c:2230–2242](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2230-L2242)、[script.c:2518–2539](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2518-L2539) |
| W12 | 启动战斗 `0x0007` 根据胜败/逃跑选择后续地址；`0x0089` 可直接设置结果、`0x008A` 启用下场自动战斗。必须区分剧情安排的败北与普通游戏失败。 | [script.c:3316–3335](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3316-L3335)、[script.c:2559–2571](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2559-L2571) |
| W13 | Windows 存档结构包括队伍、角色、背包、场景、对象及事件；加载会恢复可变对象和事件。原版 `.rpg` 存档与首版工作台 `.sav.json` 不可互称兼容。 | [global.c:530–558](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L530-L558)、[global.c:706–716](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.c#L706-L716) |

移动 opcode（例如 `0x0010/0x0011/0x007C/0x0082`）会在未到达时保留当前指令，空间条件 `0x0083` 也可能决定分支：[script.c:677–703](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L677-L703)、[script.c:2259–2274](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2259-L2274)、[script.c:2437–2470](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L2437-L2470)。文字模式可以另行定义简化空间模型，但只有与目标录制匹配后才能确认该段通过。

## 主线里程碑候选

以下是按 PAL1 通行故事记忆整理的**未验证测试导航**，不是从当前目标数据提取的真实路径，不是准确 scene/script ID，也不保证柔情篇的具体顺序、事件归类和不同版本分支相同。所有行仍待目标发行身份、连续基线与具体路线定位。当前样本已可导入，后续须用场景、事件根和消息 ID 替换记忆描述；不能把列表本身当覆盖证据。

| 候选 ID | 故事阶段提示 | 应采集的可观察结果 |
| --- | --- | --- |
| M01 | 余杭客栈、仙灵岛求药与返回余杭后的变故 | 开场入口、调查/触碰交互、物品取得/使用、关键人物对话入口变化 |
| M02 | 苏州与林月如、林家事件、隐龙窟调查阶段 | 入队/离队、调查分支、场景往返、对话人物及头像模式 |
| M03 | 白河村、玉佛寺、将军冢与血池阶段 | 任务物品前置、场景入口重写、关键战斗前后消息序列 |
| M04 | 鬼阴山及离开该阶段 | 剧情战斗结果分支、队伍状态、事件消失/入口变更 |
| M05 | 扬州、蛤蟆山、京城刘府和彩依相关阶段 | 调查链、物品条件、重复调查和剧情转换；相邻顺序待数据确认 |
| M06 | 蜀山与锁妖塔阶段 | 地图进入条件、救援相关触发链、关键剧情战斗和人物状态变化 |
| M07 | 圣姑处及神木林等求药阶段 | 队伍切换、任务物品收集/交付、已交付后重复对话 |
| M08 | 大理、麒麟洞、女娲庙及回到过去阶段 | 时间/场景切换、同一地点不同时态的事件集、关键物品保留/变化 |
| M09 | 灵珠、求雨与南诏终章阶段 | 灵珠条件、进入终章前状态、强制事件、终战结果和结局消息 |

不要用这九行作为“九个主线任务”的准确总数。应把真实主线拆到可重放的场景/入口/分支级节点，并保留失败、拒绝、逃跑和重复对话等旁路。

## 支线发现与覆盖口径

当前没有可确认的柔情篇完整支线名单；不根据记忆编造条目或总数。应从目标数据建立场景根清单，识别主线之外的 NPC 调查、自动触碰、物品交付、购买/装备条件、隐藏交互与重复访问变化。一般 NPC 闲聊也应记录，分类规则需明确，避免仅因没有任务名就被遗漏。

根清单不能只含场景/NPC：还需对象中的玩家濒死/同伴死亡入口、物品使用/装备/投掷/描述入口、法术使用/成功/描述入口、敌人回合/就绪/战后入口、毒状态入口，以及执行时改写得到的新入口。静态图中 `0xFFFF` 出现在自动脚本时还可能是 Windows 物品描述，而非场景对话：[global.h:125–216](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/global.h#L125-L216)、[script.c:3609–3638](https://github.com/sdlpal/sdlpal/blob/d4bf69b64d335cd680555af9df90e7ec4f3006a3/script.c#L3609-L3638)。不要将描述文本自动归为已发现支线。

每个候选支线登记：目标数据哈希、场景/事件 ID、入口地址、消息 ID 候选集合、前置状态、物品/钱款/队伍条件、选项、状态改动、返回主线位置、是否可错过、是否可重复、已有运行证据。主线/支线/一般 NPC 分类不确定时标为“待分类”，不会从覆盖分母中删除。

最低条件组合包括：未接取→接受/拒绝；物品不足/刚好足够/多于所需；仅背包持有/装备中持有（语义不同）；完成后重复交谈；离开再回来；存档并重开；不同队伍组成。被剧情时限或场景重写永久关闭的内容，应记录关闭条件，不直接称为“死脚本”。静态图上的不可达结论需要完整根集与状态条件证明。

## 静态审计和真实流程验证分开

| 等级 | 可以证明 | 不能据此声称 |
| --- | --- | --- |
| D0 数据清单 | 此次输入的文件名、长度、SHA-256、布局/编码检查结果 | 已运行任何剧情 |
| D1 静态审计 | 根地址、指令边界、引用范围、候选消息和 opcode 直方图 | 所有分支可达、剧情顺序正确、主支线通过 |
| D2 合成回归 | 自造脚本下某条 opcode、调度/状态语义符合测试定义 | 原版场景实际一致 |
| D3 目标流程回放 | 指定数据/起始状态/输入序列下，真实基线与 Go 记录相符 | 未覆盖支线、其他版本或未执行分支正确 |
| D4 覆盖结论 | 在公开列出的根与场景范围内，满足判定标准的流程数量/清单 | 全版本、无限状态组合的完整正确性 |

单段 D3 通过必须有：目标数据哈希和语言配置；原版或经明确记录的参考引擎版本/构建模式；起始状态/存档哈希；有序输入（调查对象、移动、菜单、选项、战斗结果、帧推进）；输出记录（tick、scene、event、script PC/opcode、消息 ID、对话模式/头像/标题、等待/翻页）；每步可变状态摘要；终点断言。参考引擎轨迹只能称为与该引擎相符，不能冒充原版二进制记录。日志可只保存 ID 和原始消息字节哈希，避免向公开仓库复制完整商业台词。

未知 opcode、步数预算耗尽、未建模移动/等待/战斗/外部交互、编码替代字符或缺根均使该路径成为 **blocked/incomplete**。允许继续收集后续日志，但不能把“跳过错误跑到停止指令”计作通过。静态候选消息数与动态实际消息数分别报告；没有目标数据时两者均写 `unknown`，不是 0 条消息。

## 交付时应填写的运行清单

以下清单用于后续验收；当前样本已完成其中的数据清单与部分导入/脚本检查，尚未完成连续流程比较：

1. D0：当前 APK 及四张数据表摘要已固定，MGO 已在本地还原并检查；仍需确认与用户指定 Windows 发行数据的关系，空间流程还需地图/碰撞资源。主程序与资源身份分别记录。
2. D1：当前已导入场景、事件、对象、角色、敌人、商店及消息，并有静态审计工具；继续补对象/自动/动态根分类，区分结构损坏、格式差异、未实现指令和未覆盖路线。
3. 先验证余杭阶段的最小“进入场景→调查 NPC→对话→入口更新→再调查”往返，再扩到 M01–M09。录制轨迹与 Go 重放都需有可复现起点。
4. 为已发现的每个支线执行正向、反向、重复、离场返回与存档恢复条件。未执行条目标记 pending/blocked；不得只统计执行过的分支而隐藏未测分支。
5. 建立结果表：`case_id / dataset_sha256 / baseline / start_state / inputs / expected_trace / actual_trace / unsupported / status`。只在无遗漏的指定范围内发布覆盖结论。

当前证据分层：

| 范围 | 当前结果 | 尚缺 |
| --- | --- | --- |
| D0 当前 APK 样本 | 文件摘要、DOS 布局、Big5、表计数与引用检查已记录 | 与原 Windows 98 发行数据的身份对照 |
| D1 当前样本 | 指令/消息统计、场景 1 入口定位、部分根前缀审计 | 完整对象/自动/动态根分类及条件图 |
| D2 指令与会话 | 合成回归覆盖部分控制流、受限自动调度/NPC 行走、状态、资源边界与回滚 | 未实现指令及原版数值规则 |
| 当前样本逻辑回放 | 真实 NewSession → 场景 1 开场脚本完成提交 → 事件 5/6/7 拾取 → 重复 5 拒绝；事件 1 离场在 PC 4669 淡出阻塞且回滚 | 空间交互可达性、后续场景资源加载及原版/参考引擎连续轨迹比较 |
| D3 / D4 | 尚无满足上述标准的连续主线/完整支线验收结果 | 原版或固定参考引擎基线、完整路线与覆盖清单 |

2026-11-06 当前样本已从真实 `NewSession` 初始状态（不声明已进入）在逻辑模型中完成场景 1 OnEnter：**PC 7952 → 8144 (`stop1`)，返回入口 8145，`completed=true`、`committed=true`**。记录 193 个顶层步骤、130 个装备子步骤、76 个 tick、60 个自动工作子步骤和 67 次消息引用；初始化另有 130 条装备指令、独立 10,000 步预算，不能混加到本次调用。结束时 NPC 11 为 `(1152,384)`、state 0、auto 4458，队伍领队为 `(1344,288)`。

随后在同一会话依次调用事件 5/6/7，完成消息 ID 1025/1026/1027、物品 99/201/236 各 1、事件隐藏及重复触发 5 拒绝且状态不变的检查。这次顺序先执行开场，没有使用 `AssumeSceneEntered`。但调查仍由事件 ID 显式调用，未验证空间触发、原版程序对照或完整主支线。

下一实际边界为事件 1 离场脚本 **PC 4669 / 0x123d，opcode 0x0050（淡出）**。此调用保留诊断前缀但不提交，已完成的开场状态和已得物品保持不变；即使补齐淡出，后续 `SceneLoadPending` 的地图/GOP/事件资源生命周期仍需实现。

初始场景的地图/GOP/事件精灵整体加载仍未实现；目前实际精灵解压接在 `0x65/0x75` 玩家资源重载，不能据此认定初始现场资源完整。渲染、碰撞及事件 `SpriteFramesAuto` 的资源回填仍待验证。

公开源码研究 W01–W13、解压差异验证、预设检查点及当前开场逻辑顺序回放均不能替代完整目标流程验证。
