Polymarket 15min BTC Up/Down
高频双向控制型交易系统 —— 技术设计文档（v1）
1. 项目背景与问题定义
1.1 市场背景

Polymarket 的 BTC 15 分钟 Up / Down 市场具有以下显著特性：

时间极短（15min 固定结算）

价格非线性映射为概率（0–100）

最终只有一个结果结算（Yes / No）

中途无法真正平仓，只能通过反向买入改变风险结构

这使得它不等同于任何传统交易市场：

不是现货

不是期货

不是期权

而是一个 “时间约束下的概率共识形成过程”

1.2 传统策略的问题

常见思路包括：

网格

方向预测

固定比例对冲

三段式（前 / 中 / 后 5 分钟）

这些方法在该市场中存在系统性缺陷：

❌ 过度依赖方向判断

❌ 无法处理单边极速共识形成（20 → 80 → 98）

❌ 对“时间压力”建模不足

❌ 缺乏系统级风险熔断能力

2. 核心思想（Strategy Essence）
2.1 本系统不做什么

❌ 不预测 BTC 方向

❌ 不赌 Up 或 Down

❌ 不依赖固定时间分段

❌ 不试图在终局前“卖出获利”

2.2 本系统做什么（核心 Alpha）

在极短时间窗口内，
将“市场共识形成的过程”，
转化为“结构性摩擦收益”。

具体表现为：

双向持仓不是对冲，而是 构造非线性 payoff

高频买入不是为了仓位，而是 控制平均成本与时间暴露

Freeze 不是止损，而是 承认世界已经确定

风控不是防亏损，而是 防系统失真

3. 系统总体架构
Market (WS / REST)
        ↓
Signal Layer (Entropy / Velocity / Acceleration)
        ↓
Brain (Continuous Controller)
        ↓
Order State Machine
        ↓
Position Truth Engine
        ↓
Settlement / End

↑                ↓
Audit Bus     Kill-Switch
↑
Simulator / Playback / Time-Travel

4. 控制系统（Brain / Controller）
4.1 连续控制，而非规则触发

系统不使用 if/else 决策，而是连续控制变量：

市场不确定性（Entropy）

价格变化速度（Velocity）

加速度（Acceleration）

剩余时间（Time Pressure）

输出为：

Up / Down 的相对买入力度

Normal / Shock 双通道混合权重

风险增减速率（Gain）

4.2 双控制通道
通道	作用
Normal	稳态吸收波动
Shock	极端行情下快速重配

最终行为为二者的连续混合，而非切换。

4.3 参数在线自适应（Auto-Gain）

市场越稳定 → Gain 自动下降

市场越混乱 → Gain 自动上升

时间越接近结算 → Gain 自动衰减

5. Freeze Detector（胜负已定检测）
5.1 定义

当市场共识高度收敛（如 98 / 99），系统判定：

胜负已定，不再值得增加任何风险敞口

5.2 特点

不可逆

一旦触发：

禁止新增仓位

只允许风险下降

属于策略层冻结

6. Kill-Switch（系统级熔断）
6.1 定义

Kill-Switch 不判断策略是否赚钱，只判断：

系统当前是否仍然可信

6.2 触发源

WebSocket 断线 / 不稳定

本地订单状态 ≠ 服务器状态

仓位真相无法确认

API 延迟或异常

时间戳漂移

6.3 特点

任意模块可上报风险

只有 Kill-Switch 有冻结权

一旦触发：

全系统冻结

停止交易

取消订单

标记仓位为“不可信”

7. 订单状态机（Order State Machine）
7.1 解决的问题

ACK 延迟

Partial Fill

卡单

Cancel / Replace 不确定性

7.2 核心原则

永远不假设订单“已经发生”

订单状态必须通过状态机推进，而非直接修改。

8. 仓位真相引擎（Position Truth Engine）
8.1 为什么需要它

交易所返回的仓位信息并非实时、也非完全可靠。

8.2 仓位来源融合

订单执行回执

WS 成交推送

REST 定期校验

当三者不一致：

降低可信度

或触发 Kill-Switch

9. Simulator / Playback / Time-Travel
9.1 Simulator

模拟真实撮合

注入异常：

WS 断线

延迟

乱序

极端单边

9.2 Playback（回放）

使用历史 Audit 数据

重放系统决策过程

用于复盘与调参

9.3 Time-Travel Debug

修改历史某一时刻参数

推演反事实结果

回答问题：

“如果当时更激进 / 更保守，会发生什么？”

10. 工程目标总结

这不是一个“策略脚本”，而是一个：

可长期运行、
可自证正确、
可持续演化的交易系统. 我们现在需要用golang  来开发一个 polymarket 15 分钟btc 的交易机器人。