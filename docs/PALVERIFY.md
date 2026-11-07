# PAL 数据与流程验证工具

`cmd/palverify` 导入本地 PAL 数据并执行有界 Go 脚本/空间模型。当前固定样本为 DOS 六 WORD / Big5 / YJ1，原 Windows 98 发行身份未确认，见 [INPUT_DATA.md](INPUT_DATA.md)。**全主线与全部支线尚未通过**；最新进度及缺口见 [BUGS.md](BUGS.md)。工具不启动 APK/原 exe，不修改游戏资源。

## 导入与审计

目录至少含 `SSS.MKF`、`DATA.MKF`、`M.MSG`、`WORD.DAT`。文件名大小写不敏感，但不能存在歧义同名项。编码必须指定；布局默认 `windows`，当前样本应选 `dos`。

```sh
export PAL_DATA=/path/to/local/pal-data
mkdir -p reports
go run ./cmd/palverify inspect -data "$PAL_DATA" -layout dos -encoding big5 -out reports/import.json
go run ./cmd/palverify audit -data "$PAL_DATA" -layout dos -encoding big5 -out reports/audit.json
```

`inspect` 输出摘要、记录数与警告。`audit` 清点指令并遍历部分入口的静态控制流前缀，不执行条件、自动调度、动态对象回调或战斗；退出0不表示主线通过。`audit -entry 123` 可指定十进制入口。

## 连续空间路线

`journey` 从真实新游戏初始化开始，保留一个 Session；后续动作不会重置库存、事件、NPC计时或返回入口。目录另需 `MGO.MKF`、`MAP.MKF`、`GOP.MKF`，全部七个文件的摘要固定在路线中。`PAT.MKF` 仅彩色视口需要，不影响该无图像报告。

```sh
go run ./cmd/palverify journey -data "$PAL_DATA" -encoding big5 -layout dos \
  -init-route reports/starter.json
go run ./cmd/palverify journey -data "$PAL_DATA" \
  -route testdata/routes/sample-opening-wine-village.json -out reports/opening-journey.json
```

路线 `version=1`，根字段为 `name`、`encoding`、`layout`、`files_sha256`、`actions`。首动作必须为 `enter`。每个动作明确给出 `inputs: []`，可选 `expected_messages`、`expected_scene`、`expected_inventory`、`expected_event_states`；`expected_event_states` 按事件ID断言精确有符号状态值，可验证机关破坏/开启。消息顺序或状态不符返回非零，报告会注明该动作是否已经提交。

| type | 参数 | 执行行为 |
| --- | --- | --- |
| `enter` | 无 | 处理必要的地图/精灵加载并执行当前场景 OnEnter |
| `move` | `direction: 0..3` | 一帧完整触碰/自动更新后，碰撞检测并移动16/8像素；0南↙、1西↖、2北↗、3东↘ |
| `interact` | 无 | 一帧更新后按朝向搜索13个位置；触碰会清除本帧调查输入 |
| `use_item` | `item`、`target_role` | 当前库存或队伍可用装备的物品脚本；人物为角色ID，全体物品须65535；成功且消耗标记时扣物品 |
| `trigger` | `event` | 明确按事件ID进行诊断，不证明距离/朝向/碰撞可达 |

示例动作：

```json
{"type":"use_item","item":272,"target_role":65535,"inputs":[],"expected_inventory":{"272":1}}
```

该物品的脚本只准备NPC交互，实际任务扣除发生在后续 `interact`，不能一概将“物品使用”视为立即交付。

Session 默认采用明确的 `auto_attack` 数值战斗策略和种子1的模型随机源；这不是原版随机分布的一致性声明。没有预先给定胜利结果。支持的回调、毒/状态、经验和奖励由 Go 执行；未实现行动或无失败分支的战败阻断。单次独立 `trace` 仍可显式提供外部战斗结果，并标记 `battle_outcomes_stubbed`。

路线仅允许严格UTF-8 JSON，拒绝重复键、未知字段、null、拼写别名及多文档，最大1MiB/10,000动作。每动作默认10,000统一工作预算，可用 `-max-steps` 设置1..100,000；嵌套脚本、装备、自动更新、帧和战斗共享预算。报告最大32MiB。`-compact` 对成功动作保留输入、真实消息ID、工作量、前后SHA-256及最终完整快照，省略完整轨迹和差异；中间状态须重跑固定路线复核，失败动作保留可用完整诊断。完整模式中嵌套 `world/idle_frames` 省略，保留初始计时表、每步状态差异、调用前摘要和最终完整快照，字段 `trace_state_encoding` 说明重建方式。报告超限不会伪称已经执行的动作被回滚。

## 单次诊断

`enter` 只执行所选场景进入；非1场景是声明检查点，不能证明之前的路线。`-mgo` 支持玩家精灵重载，不自动给这个旧单次命令提供完整地图资源；完整空间路线使用 `journey`。

```sh
go run ./cmd/palverify enter -data "$PAL_DATA" -layout dos -encoding big5 \
  -mgo "$PAL_DATA/mgo.mkf" -scene 1 -out reports/opening.paltrace.json
```

`trace` 路线格式与 `journey` 不同：固定四表摘要、`request`（入口/事件/计时/输入及可选世界快照）和 `expected_messages`。`event_id=65535` 复用末次上下文，0表示无事件上下文；补充状态是调用者声明，不能当作真实可达检查点。

输入有 `confirm` 0/1、`key` 1、`random`、`shop` 0关闭或物品对象ID、`battle` 0胜/1败/2逃。买卖按实际需要另外消费确认输入，缺钱不询问确认；重复选择可多次交易，整个菜单失败回滚，选择计入玩家子工作预算。卖出菜单按原语义压缩零数量槽；购买在满包/99上限时仍扣钱，此参考行为保留并记录。通用随机输入允许0..2147483647，由指令进一步验证具体区间与参考整数除法尾部边界。直接 `trace` 的旧路线解析器对某些随机输入仍有更窄限制；连续路线按当前 `journey` 规则。默认隐藏正文，`-include-text` 仅本地核对，请勿提交商业台词。

## MyGo 原版数据页

读取同一数据目录后建立会话。`PAT.MKF` 可用时显示320×200地图、人物/事件叠层，提供方向、调查和物品使用。视口为静态资源预览，尚未实现淡入淡出、波纹、染色、RNG过场或音频。来源选项改变后须重新读取，界面场景下拉不会传送现有会话。

“声明场景已进入”与下方事件ID调用仅供诊断。导出包含动作意图、真实调用请求/选项、前后状态及是否提交；失败前缀供排查，活动会话只提交完整动作。已支持[工作台 JSON 会话存取](SESSION_SAVE.md)，原版 RPG 存档尚未兼容。

### 快速验证战斗

```sh
go run ./cmd/palverify journey -data "$PAL_DATA" \
  -route testdata/routes/sample-opening-island-landfall.json \
  -battle-mode fast_verify -compact -out reports/fast-journey.json
```

默认 `auto_attack` 使用导入数值。`fast_verify` 仅在独立战斗副本中将非零敌人模板 HP 降为 1，召唤和变形使用同一副本；行动顺序、战斗脚本、奖励和胜败分支仍实际执行，失败不会被替换为胜利。此策略可能减少回合、改变 HP 条件分支，不用于原数值平衡验收。报告记录实际 `execution_policy.battle_mode`、简化范围和每场 `battles[].mode`。MyGo 提供相同开关，在重置会话后应用。

`PAT.MKF`、`RNG.MKF`、`FBP.MKF` 按存在情况自动载入，相关指令要求实际资源。报告列出实际摘要；新路线可通过 `optional_files_sha256` 固定这三项，`-init-route` 自动记录已提供的项。旧七文件路线仍可执行，但没有固定其未列出的媒体资源。

## 本地回归与退出码

```sh
PAL_GAME_DATA_DIR="$PAL_DATA" go test ./cmd/palverify -run TestLocalJourney -v
PAL_GAME_DATA_DIR="$PAL_DATA" go test ./internal/palgame -run TestLocal -v
PAL_MAP_ASSET_DIR="$PAL_DATA" PAL_PIXEL_ASSET_DIR="$PAL_DATA" go test ./internal/palasset -run Local -v
```

无对应资源时本地回归跳过，CI合成测试不能替代实际样本结果。固定客栈路线546动作/194消息引用，最终场景4、现金550、桂花酒归零。新增 `sample-opening-island-landfall.json` 连续1084动作/372消息引用，终点场景15 `(752,808)`、破天锤279×1，可配合 `-compact` 使用。精简报告也保留 `battles`，区分Go数值结果 `numeric` 与外部给定结果 `supplied`。未执行原 APK/PC 程序，也没有完成全主支线。

未知指令、越界、预算、缺失/剩余输入和断言错误返回非零；完整 Session 动作失败回滚。`completed` 只代表所请求模型动作完成，所有报告继续保留 `full_game_verified:false`。

## APK 静态初筛

```sh
go run ./cmd/palverify apk -file /path/to/game.apk -out reports/apk.json
```

有界检查归档元数据、摘要、LFS指针与数据文件名候选，不安装/启动目标。REA MCP当前不可调用，使用独立Go/Python工具和固定公开参考资料；不能将这些结果写成实际REA引擎运行证据。
