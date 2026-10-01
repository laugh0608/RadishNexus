# ADR-0025：Project 与 Channel 最小发现入口

状态：已接受

日期：2026-09-10

## 背景与范围

项目所有者已要求继续最小使用入口。当前首页必须输入稳定 ID，普通成员无法发现自己可读的 Project 与 Channel。本切片增加只读列表和首页导航，复用现有 Session、Workspace、Project 与 Channel 权限，不增加数据表、依赖或角色能力。

Project / Channel 创建、Project 角色配置、受限 Channel 成员配置和其他对象列表属于后续写入与发现切片；本次结果不代表空实例初始化和完整成员治理已经完成。

## 公共列表合同

- `GET /api/v1/workspaces/{workspace_id}/projects` 返回当前可读 Project。
- `GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/channels` 返回指定可读 Project 内的可读 Channel。
- 仅接受 `limit` 和 `after`，默认 25、最大 50。重复、未知、空值或非法 query 返回 `400`。`after` 为有版本的 opaque base64url cursor，绑定 Workspace、列表种类和 Project；它仅表示排他分页边界，不承担授权。
- 结果为 `{"data":{"items":[{"ref":{"type":"project","id":"prj_example"},"title":"示例项目","status":"active"}],"next_cursor":null}}`。Channel 使用同形安全条目和 `channel / chn_`；status 仅 `active / archived`。
- 按稳定 ID 升序 keyset 分页；先过滤当前权限，再取 `limit + 1` 判断下一页。不返回隐藏对象占位、隐藏数量、总数量、权限表、成员信息或正文。cursor 只取已返回的可读条目边界。
- 每页按一致的当前权限快照判断：必须为 active Workspace 成员；Project 为 `workspace` 可见或有显式 Project 成员关系；restricted Channel 还要求显式 Channel 成员关系。Workspace owner / Project admin 不绕过窄权限。归档保持可读并标记状态，写入仍由既有命令拒绝。
- 不可访问 Workspace / Project 与不存在者均为 `404`；失效 Session 为 `401`。精确 Host、可信代理、Session Cookie、安全错误 envelope、`private, no-store` 与 `Vary: Cookie` 沿用既有读取 transport；不引入 CORS 或新的身份入口。

## Web 行为

首页展示 Workspace 选择、Project 分页和选中 Project 的 Channel 分页，Channel 直接进入 canonical 页面。按 ID 打开既有协作对象与 Deployment 的工具保留为次级入口，账户与邀请继续使用已有页面。

切换 Workspace / Project、翻页、主动刷新、页面重新获得焦点时重新读取并清理旧列表；每次只展示一个查询页，不拼接旧页。请求可取消，迟到响应不能覆盖新作用域。错误和撤权清空数据，Session 失效回到登录；空列表只说明当前没有可读对象，不推断其他对象不存在。当前没有权限变更实时推送，不能把页面上曾经可见的条目当成继续访问的凭证。

## 验证与兼容性

真实 PostgreSQL 验证跨 Workspace、暂停成员、restricted Project / Channel、owner / admin 不穿透、归档、分页边界、权限撤销和列表与既有读取语义一致；代表性列表数据记录 SQL 次数和耗时。HTTP 覆盖匿名、Host / proxy、严格 query / cursor、错误收敛与安全 DTO；Web 覆盖正常导航、分页、切换、迟到响应、失败清理和失效 Session。

此为增量 GET 合同，Go 与 Web 应一起发布；旧 Web 可继续使用既有入口，新 Web 需要新服务端。无需 migration 或存量数据回填；真实浏览器、空实例配置与团队持续使用另行记录证据。
