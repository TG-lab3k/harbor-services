# harbor-services

## 简介

- harbor-services 是一个多租户轻量级 BaaS 中台服务。
- 为独立开发者的众多产品提供「统一注册/登录 (Auth) + 支付 (Billing) + 运营配置 (Online Operations)」轻量级 BaaS 中台，以便减少各个产品通用功能的重复开发，节约时间成本，能快速上线新产品。

## 技术栈

- 开发语言: Go
- 框架: Gin
- 部署平台: Google Cloud Run
- 数据库: Google Firestore
- 容器化: Docker
- 接口风格: REST API

## 模块

- 租户 (APP) 管理 P0
- 统一注册/登录 (Auth) P0
- 支付 (Billing) P0
- 运营配置 (Online Operations) P1
- Admin 管理后台 P0

## 文档

- [架构](docs/ARCH_README.md)
- [API](docs/api.md)
- [部署](docs/DEPLOY.md)
- [重新部署](docs/RE_DEPLOY.md)

