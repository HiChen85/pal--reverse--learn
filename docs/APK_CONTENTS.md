# 用户提供 APK 的资源静态检查

该 APK **不能直接作为当前 PAL98 导入器的数据目录**。ZIP 成员中没有 `SSS.MKF`、`DATA.MKF`、`M.MSG`、`WORD.DAT`，抽样看到的是另一套场景二进制、gzip 包装的 protobuf 数据、Cocos 风格 UI 和贴图资源。它包含可继续离线研究的内容表，但当前证据不足以证明它包含 PAL98 柔情篇的完整主线、支线和运行逻辑，也不足以证明脱离服务器能完整游玩。

## 输入与方法

- APK SHA-256：`9152a9ccc8f22c2a68538f3e93f5da66dec99f0b36440e7390559cd53570efc4`；归档字节数：348,830,394。
- 依据完整 ZIP 成员清单以及 13 个成员的有界抽样。样本单个压缩后不超过 512 KiB，gzip 单个解压不超过 4 MiB。
- 工具：Python 标准库 ZIP/gzip、SHA-256、XML plist 和手写有界 protobuf wire 元数据读取。当前没有实际 REA MCP 引擎；这些结果是手动静态分析，不是 REA 工具输出。
- 没有安装或启动 APK，没有访问任何服务，没有尝试取得密钥、解密受保护容器或绕过账号/签名/DRM。样本没有显示加密保护：高熵 `.proto.dat` 样本具有普通 gzip 文件头，按公开压缩格式成功解压。
- 原始样本和详细元数据只留在本地 `/workspace/apk-analysis/assets/`。本文仅记录数量、结构、签名和哈希，不提交商业素材或完整对白表。

## 直接观察

ZIP 共 14,445 个成员，声明的未压缩总长度为 402,776,228 字节。扩展名分布包括 7,731 PNG、2,847 BIN、2,834 CCBI、227 plist、231 OGG、148 DAT、38 SO。完整成员名称按大小写不敏感的 basename 检查，四个 PAL98 必需文件均未匹配。这里只证明成员命名不匹配，没有把它扩张成“任何内部数据都绝不存在原版内容”的结论。

未发现 `.lua`、`.luac`、`.js`、`.jsc` 后缀成员，但存在其他后缀的脚本：`assets/bin/xjccbtest.bin` 以 `1b 4c 75 61 51` 开头，是 Lua 5.1 标准字节码头；后续头字段显示小端、4 字节整数/size_t/指令、8 字节非整数数值。仅识别文件头，没有执行字节码。

`assets/story/` 有 2,797 个 `.bin`，`assets/publish/story/` 另有 47 个。三个抽样文件具有下列一致的可读结构前缀：

| 成员 | 长度 | 首个 BE uint32 | 随后的两个 BE uint16 长度字符串 |
| --- | ---: | ---: | --- |
| `assets/story/scene_405.bin` | 605 | 405 | 3 字节标识、3 字节标识 |
| `assets/story/scene_2806.bin` | 2,522 | 2,806 | 7 字节标识、3 字节标识 |
| `assets/publish/story/scene_280001.bin` | 1,281 | 280,001 | 6 字节标识、3 字节标识 |

首个整数均匹配文件名中的场景编号。其余字节尚未建立完整格式语义；这些前缀支持“可结构化分析的场景文件”推断，不构成主线顺序、完整对白或可执行分支的证明。

`assets/publish/story/story0.plist` 可按 XML plist 解析，根键为 `frames` / `metadata`，包含 15 个贴图帧，是图集描述，不能因名称含 story 就当作剧情脚本。`assets/publish/story/bg/zhujiantai_zhenyaoj.ccbi` 的前四字节为 `69 62 63 63`（`ibcc`），配合 CCBI 后缀和 UI/场景资源树，与 CocosBuilder 资源形式相符；未完整解析该二进制格式。

## gzip / protobuf 样本

五种 `.proto.dat` 样本都以 `1f 8b 08 08` 开头，按普通 gzip 解压成功。解压数据均能被通用 protobuf wire 读取器完整遍历，但没有取得字段 schema，因此不能把字段数直接解释为章节数、关卡数或对白数。

| 成员（`assets/proto_a_q/` 下） | gzip 长度 | 解压长度 | 可验证的 wire 结构 |
| --- | ---: | ---: | --- |
| `story.proto.dat` | 90 | 375 | 顶层 field 1 varint 1 次、field 2 length-delimited 12 次 |
| `scenarioInfo.proto.dat` | 1,093 | 5,062 | field 1 varint 1 次、field 2 length-delimited 20 次 |
| `chat_story.proto.dat` | 12,649 | 164,381 | field 2 出现 594 次、field 3 出现 1,320 次；均为 length-delimited |
| `stage.proto.dat` | 427,432 | 3,590,093 | 共 25,626 个顶层字段实例 |
| `battle.proto.dat` | 285,882 | 4,050,643 | 共 35,317 个顶层字段实例 |

`assets/proto_i_q_s/story.proto.dat` 与 `proto_a_q` 的相应成员 SHA-256 完全相同；其余同名文件只确认清单中的长度一致，没有据此声称全部内容相同。

| 样本 | ZIP 内成员内容 SHA-256 |
| --- | --- |
| `proto_a_q/story.proto.dat` | `0dc59b6096c1d07f7761d0fb89561cc2140ed02d82009c8bb3ec889392203bff` |
| `proto_a_q/scenarioInfo.proto.dat` | `b7f3c2cb565fbe906ab6b9a1c9c6112e983ee8fb201493dc45ad00da99b12530` |
| `proto_a_q/stage.proto.dat` | `80d5d91f94a4e1e7fb342e1ec12523339e2b84ddc00959070d2cc20ddc19de06` |
| `proto_a_q/chat_story.proto.dat` | `aa28f490462e44acc282c1e11120b933837c0030e596f99a6dba67993ef2e3a2` |
| `proto_a_q/battle.proto.dat` | `25d115e4be61bbaadb712d782444e37166524d2b580d1da5a4839b0faa882a6c` |
| `assets/bin/xjccbtest.bin` | `9b60e2e3e6d4e2cf961355ba93be8f940255494058176b9f0b8d4dfbb0a7d3dd` |
| `assets/story/scene_405.bin` | `fbecbef53a3994174e253153aa230b3945550a0a4a72e83478d4f270413853c1` |
| `assets/publish/story/story0.plist` | `df53ff624405b3db4544fd78e3e576f2a97d6b37b7259d763481fb61ad9a2ae0` |

## 对项目的含义

成员名包含 `Card`、`Equip`、`Payrank`、`xianmeng`、`vip.proto.dat`、MSDK/QQ 相关 UI，以及多个仙剑系列场景名。结合 Cocos 风格场景和 protobuf 表结构，合理推断这是包含卡牌、装备、社交/运营功能的手游客户端资源，而不是现有 Windows PAL98 数据格式的直接重新封装。确切包名、版本、原生库与服务器依赖应结合 manifest/native 分析判断；资源名称本身不能证明实际联网行为或是否必须联网。

继续做这个手游的兼容研究，需要建立其场景记录和 protobuf schema、查明脚本与客户端/服务端的职责，再验证所需资源与分支是否齐全。即使完全读出这些本地表，也不能自动等同于 PAL98 的主线/支线；两个目标应保持独立。当前 PAL98 路径仍需要合法持有的 Windows 原版资源目录，再由只读导入、opcode 审计和指定路线轨迹逐项验证。
