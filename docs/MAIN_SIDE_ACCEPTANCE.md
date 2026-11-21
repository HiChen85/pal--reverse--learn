# 主线与支线验收记录

本次验收先检查主线与可选任务共用一个会话时的行为。历史样本回放、合成兼容测试和原创工作台测试各自记录结果。

| 验收对象 | 范围 | 结果 |
| --- | --- | --- |
| 历史 PAL1 样本 | 开局至蛇洞的固定清单，7 段、7,304 动作、2,011 次消息引用 | 旧记录已通过。本次缺少匹配资源，未重放 |
| 原版全部主线和支线 | 原版发行数据及各分支实际执行 | 未完成验收。旧样本是 DOS 布局、Big5、YJ1，发行身份未确认 |
| 兼容运行器的合成数据 | 同一会话里调查主线门禁，分别走支线修复或领取普通通行证 | 通过。修复路线 4 段、15 动作，跳过路线 3 段、7 动作 |
| 工作台原创内容 | 完成或跳过支线、回到同一主线、战斗前置、重复访问、读档和道具结转 | 通过 |
| 地图标记 | 显式从指定节点开始独立试玩 | 已覆盖这个入口行为。它会建立新试玩，跨地图标记的连续任务状态尚未实现 |

2026-11-21 的工作区没有原先导入的样本。GitHub 主分支只含另一份 APK 的 LFS 指针。原样本页面可读取，下载端此次返回 HTTP 503。用空目录实际调用验收命令时，执行器在 `missing SSS.MKF` 的预检阶段停止，未建立报告目录。已重新检查历史清单的 7 段输入、来源摘要和逐动作消息断言。这个检查只确认路线文件完整。

旧样本对应 `SSS.MKF`、`DATA.MKF`、`M.MSG`、`WORD.DAT`、`MGO.MKF`、`MAP.MKF` 和 `GOP.MKF`，以及清单固定的 `PAT.MKF`、`RNG.MKF`、`FBP.MKF`。从匹配目录启动已有回归时使用下列命令。输出目录应为新的目录。清单和样本身份见 [输入数据记录](INPUT_DATA.md) 与 [已固定路线](../testdata/routes/sample-opening-snake-cave-campaign.json)。

```sh
palverify campaign -data "$PAL_DATA" \
  -manifest testdata/routes/sample-opening-snake-cave-campaign.json \
  -out-dir "reports/snake-cave-new-run" -battle-mode fast_verify -max-steps 100000
```

`fast_verify` 降低开战时敌人 HP。数值回合、剧情脚本和奖励仍会执行，战斗平衡和所有伤害分支需要另外验证。报告的 `completed` 只适用于输入清单，`full_game_verified` 保持 `false`。原版支线也需要各自的实际输入和预期结果，包括跳过、拒绝、交付、重复调查与返回主线。见 [分段验证说明](PAL_CAMPAIGN.md)。

回归可以单独执行：

```sh
go test ./cmd/palverify -run '^TestCampaignOptionalSideQuestAndMainGate$' -v
go test ./internal/studio -run '^TestOptionalQuestRejoinsMainStory$' -v
```

兼容运行器测试自行生成资源表，通过正常移动和调查选择事件，并检查每步的消息、库存、事件状态和快照摘要。缺材料时无法交付，有材料而拒绝时库存不变。交付只扣一份材料，复查奖励不重复。两条路线都进入同一场景。工作台测试从新会话分别走完有支线和无支线的路线。完成支线后重新打开任务地点，完成选项仍保持隐藏。主线战斗会保留支线消耗的物品数量，读档后能再次进入同一场主线战斗并到达相同结局。
