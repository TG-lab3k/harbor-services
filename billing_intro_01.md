# harbor-billing

## 简介&背景
-  harbor-billing是一个MoR收款/支付网关(接入stripe)中台saas服务
-  目标: 统一接口 + 降低新产品对接成本 + 降低切换代码成本
-  背景: 我是一个独立开发者，不断有新产品(saas， app)推出，减少对收款功能的对接成本，同时可以让产品选择不同的MoR平台。
-  规划: 前期只对接MoR（Merchant of Record）平台，后期会使用支付网关(stripe)替换MoR，让产品在收款层面做到无感知。
-  MoR接入平台: Creem, Waffo Pancake,Paddle。

## 需求
-  用户端: 为Micro saas，h5， app提供收款服务。
-  管理端(Admin): 
    -  a. 提供MoR平台需要的配置信息，包括账户信息，app id，key等MoR平台所需。
    -  b. App可以切换MoR平台。

