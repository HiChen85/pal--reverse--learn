# PAL 数据与触发脚本检查工具

`cmd/palverify` 用于记录可复现的数据导入与部分脚本执行证据，**不是已经运行全部游戏的播放器**。当前已导入用户指定页面中的 APK 数据；样本采用 DOS 六 WORD 对象布局、Big5 与 YJ1，原 Windows 98 发行身份仍未确认。身份与哈希见 [INPUT_DATA.md](INPUT_DATA.md)。原创 `palstudio` 示例仍可独立运行。

## 输入与导入

数据目录至少包含 `SSS.MKF`、`DATA.MKF`、`M.MSG`、`WORD.DAT`。文件名大小写可不同，但不得出现大小写歧义的同名文件。`-encoding` 必须明确选择 `big5` 或 `gbk`；`-layout` 默认 `windows`，当前样本须显式使用 `dos`。配置是解释输入格式的选择，不是发行版本认证。工具不启动原 exe/APK，也不修改游戏资源。

```sh
export PAL_DATA=/path/to/local/pal-data
mkdir -p reports
go run ./cmd/palverify inspect -data "$PAL_DATA" -layout dos -encoding big5 -out reports/import.json
go run ./cmd/palverify audit -data "$PAL_DATA" -layout dos -encoding big5 -out reports/audit.json
```

`inspect` 输出文件 SHA-256、表记录数量与警告，不输出对白正文。`audit` 输出全表 opcode 直方图及场景进入/传送、事件触发入口的已建模前缀图。自动脚本使用不同调度，不混作 trigger 根；对象物品等根尚未全部分类。因此静态图不是完整主支线图。`audit -entry 123` 可定向检查十进制 PC；未知指令或需要世界上下文的指令形成 blocker。audit 退出码 0 表示工具完成，不表示剧情通过。

## 新游戏场景进入诊断

`enter` 从导入的初始表和参考引擎默认值建立逻辑会话，先执行有界的新游戏装备重建，再执行所选场景的 OnEnter。默认场景 1；选其他场景是声明检查点，不证明此前路线可达。`-mgo` 可接本地完整 `MGO.MKF`，让 `0x65` 精灵设置和 `0x75` 换队执行实际 YJ1 解码，报告只保存资源摘要、长度与帧数，不渲染图像。

```sh
go run ./cmd/palverify enter -data "$PAL_DATA" -layout dos -encoding big5 \
  -mgo "$PAL_DATA/mgo.mkf" -scene 1 -out reports/opening.paltrace.json
```

没有 MGO 时，遇到需要加载精灵的指令会明确阻塞。APK 中若文件分成 `.part1`、`.part2`，应在本地按数字顺序还原；不能把单独一片当完整 MKF。当前读取器不实现 Windows YJ2、地图碰撞、图像/声音播放或原版战斗。

输出含独立的 `initialization`、MGO 身份及执行配置，以及 `before`、`report`、`after` 与 `committed`：失败前缀仍保留，但失败/不完整调用不会提交活动会话状态。后续场景有待加载地图时，也会阻止绕过资源生命周期直接 Enter。步骤、装备子调用、帧及自动脚本工作使用统一有界预算。`0x0009` 目前仅支持 operand 1/2 都为零的无触发更新/无队伍姿态等待，按事件顺序执行受限自动指令 `0/1/2/3/6/9/0x10/0x49`，以及显式 DOS 自动消息模式下只推进 PC 的 `0xFFFF`；自动调用 `0x4` 等其他指令仍阻塞。随机分支消费显式输入，失败会回滚整条等待。不能将未知操作当 no-op。

`0x70` 另支持单人、无跟随者的队伍行走，推进受限自动更新并记录状态；`0x6C` 支持相对 NPC 位移及动画。多队员跟随、一般触发更新与空间可达性不因此自动获得支持。

初始场景的地图/GOP/事件精灵整体加载仍未实现。当前 `-mgo` 支持的实际加载是玩家精灵重载，不等于场景已能绘制；事件精灵帧数的资源回填、碰撞与渲染仍待验证。

## 有输入和预期的单次脚本记录

`trace` 用路线文件固定四个数据文件的摘要、入口、显式输入和预期消息 ID。以下只是格式占位，不能作为原版开场路线：

```json
{
  "version": 1,
  "name": "replace-with-verified-case-name",
  "files_sha256": {
    "SSS.MKF": "replace-with-import-report-sha256",
    "DATA.MKF": "replace-with-import-report-sha256",
    "M.MSG": "replace-with-import-report-sha256",
    "WORD.DAT": "replace-with-import-report-sha256"
  },
  "request": {
    "entry": 123,
    "event_id": 1,
    "last_event_id": 0,
    "idle_frames": {},
    "inputs": [{"kind": "confirm", "value": 1}]
  },
  "expected_messages": [10, 11]
}
```

```sh
go run ./cmd/palverify trace -data "$PAL_DATA" -layout dos -encoding big5 \
  -route /path/to/case.json -out reports/case.trace.json
```

`event_id` 为事件表的 1 基编号；0 表示没有事件上下文。缺失的触发空闲计数从初始事件表填充；显式 override 是调用者声明的检查点状态。世界/库存相关指令还需完整的相应快照；单次 trace 不自动建立可持续的原版世界或空间可达性。

输入按消费顺序给出：`confirm` 0=否/1=是；`random` 1..100；`battle` 0=胜/1=败/2=逃。战斗结果是外部假设，输出标记 `battle_outcomes_stubbed`，不证明 Go 战斗数值兼容。随机结果也不是原版 RNG 校验。默认只输出消息 ID 与原始字节摘要；`-include-text` 用于本地核对正文，请勿提交商业台词。报告、`.trace.json`、`.paltrace.json` 与原始资源已加入忽略规则。

## MyGo 原版数据页

“原版数据”页提供目录、编码、布局、场景/事件选择和显式输入 JSON。可记录单次调用，或启用逻辑会话持续保存已完成调用的库存、事件状态及返回入口。修改来源配置后必须重新读取；改变场景选择不会隐式传送当前会话。

“声明场景已进入”跳过 OnEnter，仅用于显式检查点。除了旧检查点回归，当前真实数据回归已从不跳过 OnEnter 的 `NewSession` 完成场景 1 开场，再依次调用事件 5/6/7 并验证库存、消息 ID、事件隐藏与重复拒绝。显式调用事件仍不检查距离/方向。JSON 导出保留调用请求、前后快照、错误和是否提交。

有本地匹配样本及 `mgo.mkf` 时可运行这两个可选回归：

```sh
PAL_GAME_DATA_DIR="$PAL_DATA" go test ./internal/palgame -run 'TestLocal(OpeningAndPickupSequence|PickupCheckpoint)$' -v
```

无本地数据时这些测试会 skip；CI 的合成回归不替代此数据回归。开场当前记录 67 次消息引用、返回入口 8145；后续事件 1 离场在 PC 4669 的 `0x50` 淡出阻塞，失败会保持此前开场与物品状态。

## 退出码与证据边界

trace 检查消息 ID 是否与预期完全相同。缺/剩输入、未知指令、越界、预算不足或消息不符返回非零；enter 的执行阻塞同样返回非零。可执行前缀保留 PC、opcode、操作数、计数、输入消费和 blocker。读取/解析等前置失败不制造伪造轨迹。

`script_stopped` / `completed` 只说明指定的部分模型调用停止；所有报告均保留 `full_game_verified: false`。开场脚本的逻辑回放已完成，全部 NPC 对话、空间流程和主支线连续路线仍未完成，当前阻塞快照见 [BUGS.md](BUGS.md)，验收口径见 [WIN98_VALIDATION.md](WIN98_VALIDATION.md)。

## APK 初筛

```sh
go run ./cmd/palverify apk -file /path/to/game.apk -out reports/apk.json
```

这个子命令只做有界归档元数据和 SHA-256 检查，不解压成员、安装、启动或访问 APK 服务；识别 Git LFS 指针及数据文件名候选。文件名齐全不表示格式或完整流程通过。后续本地提取/解码另由纯 Go/Python 工具完成，未运行实际 REA 引擎。
