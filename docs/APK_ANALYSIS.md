# 所提供 APK 的身份与静态分析

## 结论

该文件的 Android 清单声明包名为 **`com.tencent.xjcard`**，版本 **`1.1.79` / versionCode `774`**；应用资源中的名称为 **“仙剑奇侠传 官方手游”**。清单、DEX 类定义及 Cocos 原生库共同表明，它是一个 Android 手游客户端样本，不能作为此前 Windows 98 柔情篇 PAL1 数据兼容验证的同一目标。

“官方手游”是资源中的自称，不是本报告对发布方真实性的认证。未验证签名证书是否属于腾讯、是否重新打包，也未验证文件名中的“98柔情篇”含义。包内可见资源结构与 PAL1 的常见 MKF/MSG 数据结构不同；未发现相应文件名不等于证明任何二次封装载荷中绝不存在相关内容。

本轮是**用户所提供样本的离线静态检查**：Python 标准库读取 ZIP、解析二进制 AndroidManifest.xml/简单 ARSC 字符串资源，以及读取 DEX 索引表与 ELF 文件头。当前会话没有 REA 分析工具，亦无 JADX 等完整 Android 反编译工具；没有安装依赖、启动 APK、反编译方法体、联系程序服务或解密载荷。REA 插件的静态证据工作流可指导调查，不能写成“REA 已运行并完成逆向”。

## 样本与证据范围

- 来源：用户仓库 `HiChen85/pal--reverse--learn` 的 `origin/master` 提交 `c196a00f9255b4d2822ceae98ed6c48357787104`，路径 `xjqxc98rqb_344982.apk` 对应的本地 Git LFS 内容。
- 实际样本大小：`348830394` 字节。
- 实际样本 SHA-256：`9152a9ccc8f22c2a68538f3e93f5da66dec99f0b36440e7390559cd53570efc4`。
- 调查日期：2026-11-05。分析不执行目标代码，仅对上述固定字节内容作结论。
- 工作区保留 ZIP 清单、解析脚本及结构化身份摘要；本报告不复制完整商业素材、台词、服务地址或清单中的敏感元数据值。

### A01：二进制 Android 清单

| 字段 | 直接观察 |
| --- | --- |
| manifest package | `com.tencent.xjcard` |
| versionName / versionCode | `1.1.79` / `774` |
| application class | `com.tencent.xjcard.MainApplication` |
| application label 资源 ID | `0x7f090004` |
| 启动入口 activity | `com.tencent.gcloud.msdk.core.policy.MSDKPolicyActivity` |
| 启动 intent | `android.intent.action.MAIN` + `android.intent.category.LAUNCHER`；同过滤器另声明 `LEANBACK_LAUNCHER` |
| 游戏 activity 声明 | `com.tencent.xjcard.xjcard` |
| uses-sdk | `minSdkVersion=1`、`targetSdkVersion=26` |
| 网络权限 | 声明 `android.permission.INTERNET`、`ACCESS_NETWORK_STATE` |

这些是清单声明。特别是 `minSdkVersion=1` 不证明能在所有 Android 版本运行，网络权限不证明实际连接成功或连接了某个服务；本轮没有设备运行记录。清单未提供用来验证 Windows 98 PAL1 版本的数据身份。

### A02：应用名称资源

`resources.arsc` 的包资源表将清单引用 `0x7f090004` 解析为字符串 **“仙剑奇侠传 官方手游”**。该值来自本样本默认配置的简单字符串 entry，不是根据 APK 文件名、搜索结果或图标猜测。未对所有资源配置、别名及 UI 文本做完整解析。

### A03：DEX 类定义与平台边界

`classes.dex` 为 DEX `035`，字符串表 57,308 项、类型表 7,867 项、类定义 6,447 项。本轮从 class_defs 表确认以下类实际有定义，区别于仅出现一个任意字符串：

- `com.tencent.xjcard.MainApplication`，直接父类 `android.app.Application`。
- `com.tencent.xjcard.xjcard`，直接父类 `org.cocos2dx.lib.Cocos2dxActivity`。
- `org.cocos2dx.lib.Cocos2dxActivity`，直接父类 `android.app.Activity`。
- `org.cocos2dx.lib.Cocos2dxRenderer`。
- `com.tencent.gcloud.msdk.core.policy.MSDKPolicyActivity`。
- `com.tencent.msdk.api.WGPlatform`。
- `com.tencent.midas.api.APMidasPayAPI`。

命名空间中，`com.tencent.xjcard` 有 50 个类定义，`com.tencent.msdk` 有 608 个，`com.tencent.gcloud.msdk` 有 30 个，`com.tencent.midas` 有 151 个，`org.cocos2dx` 有 41 个。统计按前缀，不能对互相嵌套的命名空间重复求和。

这些证据确认样本包含腾讯 MSDK、Midas 和 Cocos2dx 相关组件。它们的命名与登录/平台、支付、图形运行时集成相符；**没有读取方法体或动态调用轨迹，不能证明某个 API 实际被调用，更不能证明服务当前可用**。清单包含入口与游戏 activity，不足以确认入口至游戏界面的实际跳转顺序。

### A04：ZIP 内容及原生结构

ZIP 目录共 **14,445** 项，按文件后缀计：PNG 7,731、BIN 2,847、CCBI 2,834、OGG 231、PLIST 227、PTX 161、DAT 148、SO 38。目录含 `assets/publish/Card/`、`assets/publish/ui/` 与 `assets/publish/story/` 等分类，另有 `assets/proto_a_q/`、`assets/proto_i_q_s/` 中的 `*.proto.dat` 文件。

`lib/armeabi-v7a/libcocos2dcpp.so` 是 32 位、小端、ARM（ELF `e_machine=40`）库；同 ABI 的 MSDK 库也具有该 ELF 架构。APK 中可见 `armeabi` 和 `armeabi-v7a` 库目录。文件名与 DEX 父类共同支持“Android Cocos 客户端”的判断；没有由文件名直接推导实现算法。

对**ZIP 成员名称**不区分大小写检查，未找到 `.mkf`、`.msg` 或名为 `word.dat` 的成员。此检查仅覆盖直接成员名：没有解密/解压专有封装中的嵌套内容。`*.proto.dat` 的存在支持协议/数据资源推断，本轮未把这些文件假定为已验证的 Protocol Buffers schema，也未据其名称声称已复现服务器逻辑或战斗算法。

## 关键成员的固定哈希

哈希针对 ZIP 解压后的成员字节，不是 ZIP 压缩数据流。

| 成员 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `AndroidManifest.xml` | 14656 | `5761f0fdf0209ae1968f45eaf0b12d746fca844b04d747225f8218cde7ea5c0d` |
| `resources.arsc` | 57252 | `7771295be1d07091f4d76c04ea8933e9d894a292a340f93e7561006c01082353` |
| `classes.dex` | 7294620 | `aa15fde24df407769e346ae8608d6d9978104ed12a66c5d1ee21d3b67de90eb6` |
| `lib/armeabi-v7a/libcocos2dcpp.so` | 20738136 | `403b08e7a7fc9e595516075f16dce8c6c769912c787da4a558411b6db8bc9111` |
| `lib/armeabi-v7a/libMSDK.so` | 62776 | `dae85e26d283614b0ca87b4ba2bcf8dc668ae389d828441b47aed861beeb1d81` |
| `lib/armeabi-v7a/libMSDKSystem.so` | 2600588 | `45e3d2604844da9f7772d22781b4fbf441841df9c50afbd0b18570e5161d8885` |

## 对原计划的影响

高置信度的观察是：给定 APK 自声明为上述 Android 手游包，含 Cocos2dx 活动与腾讯平台组件，直接 ZIP 资源布局不提供 PAL1 常见文件名。由此可推断它与 Windows 98 柔情篇需要分别建档，不能用这个 APK 的身份或资源目录关闭 [Windows98 验证队列](BUGS.md) 中的目标数据缺失问题。

仍未回答的问题包括：完整原生游戏模块、脚本/资源封装格式、客户端/服务端职责、战斗算法、目标 APK 是否可离线运行、服务器当前状态及签名发布者。这些需要后续明确范围的证据，不能由 SDK、资源名或类名直接推断。

用户已确认继续 PAL1 / Windows 98 柔情篇目标，稍后更换资源，因此本 APK 调查止于身份与目标不匹配的证据记录，不展开手游功能追踪。Windows 98 主支线验证仍待对应合法数据和版本身份。任何真实运行、安装或联网交互都不属于本轮已执行工作。


## 可复现的自编检查工具

仓库中的 Python 标准库脚本无需下载依赖，默认只输出白名单身份字段、类定义计数及关键成员哈希，不导出完整清单属性、元数据值、服务地址或资源文本。

```sh
python3 tools/apk/inspect_manifest.py --apk /path/to/supplied.apk
python3 tools/apk/inspect_dex_identity.py --apk /path/to/supplied.apk
python3 -m unittest discover -s tools/apk -p 'test_*.py' -v
```

两个脚本都可加 `--output /path/to/identity.json` 保存同一 JSON 输出；不会执行 APK。已对本样本运行并得到上文身份及哈希。11 项合成回归覆盖属性按名称匹配（故意颠倒 min/target 顺序）、敏感 metadata 不进入摘要、重复清单拒绝、坏 chunk 边界、简单 ARSC 标签解析及坏 DEX 头拒绝，全部通过。`minSdkVersion=1` 已按 Android 命名空间中的属性名核对，类型为整数 `0x10`、值为 `1`，不是靠属性位置推测；仍只是声明。

这些是聚焦身份识别的辅助解析器，不是完整 AXML/ARSC/DEX 验证器：ARSC 仅解析直接简单字符串 entry，复杂 entry、稀疏/offset16 表或别名引用返回 unresolved；DEX 只读主 `classes.dex` 的普通小端表及选定类，不解析方法体、不验证签名/所有校验和，也不完整处理 MUTF-8 字符串语义。已报告的类描述符是 ASCII。APK 签名真实性、运行兼容与代码行为不在工具输出能证明的范围内。

主 agent 复审后补充：两个 Python 工具在 ZIP 分配前执行中央目录预检（输入 ≤1 GiB、目录 ≤64 MiB、条目 ≤100,000；不支持 ZIP64），并采用限 JSON 后缀的原子输出，拒绝覆盖输入及其软/硬链接。新增提前拒绝坏目录、目录条目计数不符、输入覆盖及写入失败保持原文件的回归。11 项 Python 测试已通过，两个工具再次读取实际 APK，身份和摘要未改变。
