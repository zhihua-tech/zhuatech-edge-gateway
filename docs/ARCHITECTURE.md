# 架构说明

Gateway Core 不依赖具体协议 SDK：Adapter 负责读取原始值，Point Mapping 归一化，Rule Engine 执行本地决策，Outbox 负责至少一次投递。真实现场部署必须由使用者根据设备手册实现或采购协议驱动，并完成电气安全与现场联调。

社区版不会伪造对 PLC/仪表的真实连接；模拟器用于功能验收和二次开发。
