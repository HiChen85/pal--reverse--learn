# Windows 98 柔情篇资源与触发脚本检查工具

`cmd/palverify` 是为后续原版流程验收准备的工具，**不是已经能运行全部游戏的播放器**。当前没有真实目标数据：所有测试使用自造的二进制表和消息，未验证实际主线、支线或原版对话可达性。原有 `palstudio` 原创示例仍可运行。

## 输入与读取

数据目录至少包含 `SSS.MKF`、`DATA.MKF`、`M.MSG`、`WORD.DAT`，文件名大小写可不同，但不得出现两个大小写歧义的同名文件。当前只读 Windows 表结构，不能自动鉴定发行版。`-encoding` 必须明确选择 `gbk` 或 `big5`；具体值待你的数据确认，不根据“98版”名称猜测。不会启动原版 exe，也不会修改这四个文件。

```sh
mkdir -p reports
go run ./cmd/palverify inspect -data /path/to/PAL98 -encoding gbk -out reports/import.json
go run ./cmd/palverify audit -data /path/to/PAL98 -encoding gbk -out reports/audit.json
```

`inspect` 输出文件 SHA-256、表记录数量与导入警告；不输出商业对白正文。`audit` 输出全部指令 opcode 直方图，以及场景进入/传送和 NPC 触发入口的已建模前缀图。自动脚本使用不同调度，未混作 trigger 执行；对象物品等入口尚未分类。因此根集不完整，静态图不是全部主支线图。

可用 `audit -entry 123` 定向检查十进制脚本索引。未实现指令会形成 blocker，并终止该条静态扩展；未知边不会被声称“不可达”。audit 退出码 0 只表示审计工具完成，不表示剧情通过。

## 有输入和预期的单次脚本记录

真实入口和期望消息序列需来自目标数据与参考轨迹。下面仅说明路线 JSON 格式；其中摘要和索引是占位，不能作为原版开场路线使用：

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
go run ./cmd/palverify trace -data /path/to/PAL98 -encoding gbk -route /path/to/case.json -out reports/case.trace.json
```

必须固定全部四份文件的摘要，避免把另一版本的路线套用到当前数据。`event_id` 是事件表的 1 基编号；0 表示没有事件上下文。缺失的空闲计数从当前初始事件表填充，显式 override 是调用者声明的检查点状态，并不证明该状态可在游戏中到达。

输入必须按调用顺序显式给出：`confirm` 0=否/1=是，`random` 1..100，`battle` 0=胜/1=败/2=逃。战斗结果是外部假设，**没有运行或验证原版战斗**，输出会标明 `battle_outcomes_stubbed`。随机结果也是输入记录，不是原版 RNG 校验。

输出默认以消息 ID 和原始字节摘要代替正文。需要在本地核对对白时加 `-include-text`。报告目录、`.trace.json` 和原版数据已加入忽略规则，不应把整份商业台词提交到公开仓库。

## 退出码与证据边界

trace 会检查实际消息 ID 序列是否与预期完全相同；缺输入、剩余输入、未知指令、消息越界、步数/调用深度/文本总量超限或预期不符都会返回非零。若有执行前缀，报告仍保存其 PC、opcode、操作数、输入消费、返回入口和 blocker，便于记录问题。读取数据、解析路线等前置失败仅返回错误，不产生伪造轨迹。

报告的 `script_stopped` / `completed` 只说明单次已建模触发脚本停止；所有报告都保留 `full_game_verified: false`。当前没有场景移动、逐帧自动脚本、现金/背包/装备状态、触发入口持久化、原版存档或真实主支线驱动；等待 opcode 0x0009 等依赖世界更新的指令不会被悄悄跳过。这些能力补齐后才能进行真实连续流程验收，详见 [验收标准](WIN98_VALIDATION.md) 和 [缺陷队列](BUGS.md)。
