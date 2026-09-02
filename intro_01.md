# harbor-services

## 简介
-  harbor-services是一个多租户轻量级baas中台服务。
-  为独立开发者的众多产品提供「统一注册/登录(Auth) + 支付(billing) + 运营配置(Online Operations)」轻量级baas中台，以便于减少各个产品通用功能的重复开发，和节约时间成本，能快速的上线新产品。

## 技术栈
- 开发语言: go
- 框架: Gin
- 部署平台: Google Cloud Run
- 数据库: Google Firestore
- 容器化: Docker
- 接口风格: rest api

## 模块
-  租户(APP)管理 P0
-  统一注册/登录(Auth) P0
-  支付(billing) P0
-  运营配置(Online Operations) P1
-  Admin 管理后台 P0

tips: 本期只开发P0模块，P1模块先不开发，在设计上要考虑扩展。

## 需求
- Auth，参考wachi-auth(工程地址： ./wachi-auth)
- 租户(APP)管理, 将wachi-auth的app创建和管理单独抽出来，以便给Auth,billing等模块共用租户。
- Admin 管理后台: 
    -  a. 登录: 只有登录，采用特定租户&特定白名单账号，与wachi-auth的Admin处理逻辑一致。
    -  b. 提供App的创建，管理，列表，和app详情页面。
    -  c. App详情: 提供Auth的三方登录client id等登录管理，billing配置管理，运营配置(Online Operations)管理。

