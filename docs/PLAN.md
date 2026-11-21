# 实现方案与验收边界

## 目标
研究仙剑奇侠传一公开重实现的结构，制作可追加章节、编辑人物/物品、运行剧情和纯 Go 战斗的桌面工作台。UI 使用 egoist/mygo 最新稳定发布 v0.7.4，Go 1.27.3。第一阶段交付可运行的原创示例闭环；不声称原版复刻完成。

## 分层与约定
- internal/content：schema_version=1 的 JSON Pack；稳定字符串 ID，不复用原版数值槽；严格解码、跨引用校验、原子保存、内嵌原创 Demo。
- internal/story：分支对话图、条件 flags、battle 节点、扩展包追加合并、存档恢复。战斗胜利后才进入后续剧情。
- internal/battle：纯 Go 的确定性回合模拟。攻击/防御/技能/物品，行动合法性先检查再修改状态；敌方自动行动；胜败和战斗日志。首版公式独立设计，不等同 PAL1 原版算法。
- internal/studio：编辑/剧情/战斗协调，UI 与 CLI 共用服务。
- internal/pal：只读 MKF 目录检查工具，验证偏移、边界和长度；首版不执行原版脚本、不解压资源。
- cmd/palstudio：MyGo 原生 UI；cmd/paltool：无显示器环境的校验与演示命令。

## 内容契约
content.Pack{SchemaVersion int, ID, Title, Start string, Characters []Character, Items []Item, Encounters []Encounter, Nodes []Node}
Character{ID, Name string; MaxHP, MaxMP, Attack, Defense, Speed int}
Item{ID, Name, Description string; HealHP, RestoreMP int}
Encounter{ID string; Enemies []Character}
Node{ID, Speaker, Text, Encounter, Next string; Choices []Choice; SetFlags map[string]bool}
Choice{Text, Next string; RequireFlags map[string]bool}
函数：Demo() Pack；(Pack).Validate() error；Load(path string)(Pack,error)；Save(path string,Pack)error；Merge(base,extension Pack)(Pack,error)。引用含 Speaker 非空时必须存在，Next 可空表示结束。Encounter 非空是战斗节点，不允许绕过胜利推进。非战斗节点 Next 与 Choices 互斥。

## 分工与审查
1. 内容 agent：content 包、示例、校验/保存/合并和测试。
2. 战斗 agent：battle 包和测试；用 content 类型构造战斗。
3. 研究 agent：固定 SDLPAL 提交、证据报告、MKF 检查工具和测试。
4. UI agent：核实 v0.7.4 API，实现原生工作台；在接口稳定后接入 studio。
5. 主 agent：story/studio、CLI、CI、整合，独立 review；发现错误退回负责 agent，复审通过才记录。

## 阶段
- S1：内容编辑→存盘/重开→分支剧情→Go 战斗→存档/恢复闭环。
- S2：DOS/Windows 版本检测、YJ1/YJ2、对象表/消息/地图/精灵/音频只读导入。
- S3：有边界的原版 opcode VM，与合法持有版本建立 golden fixtures、逐场战斗回放比较。
- S4：地图探索、渲染/音频、平衡工具、原版终章后的扩展入口与内容迁移。

## 证据与发布
REA 当前无实际 MCP/引擎工具，只能应用其证据管理方法；源码研究用普通工具。不得把源码推断写成二进制或运行证据。只包含原创演示内容，不提交原版商业素材。用户已允许公有仓库，实际目标为 `HiChen85/pal--reverse--learn`；保留该仓库的 MIT 许可，代码独立编写，不移植 SDLPAL 的 GPL 实现。
