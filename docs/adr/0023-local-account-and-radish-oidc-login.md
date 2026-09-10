# ADR-0023：本地账户与 Radish OIDC 联合登录

状态：已接受（本地账户本轮实施；Radish OIDC 作为未来规划）

日期：2026-09-10

Supersedes：ADR-0012 中不可变登录名、身份状态依附密码账户的决定；ADR-0013 的登录请求字段由本 ADR 更新。既有 Session、CSRF、当前权限、可信代理与限流边界继续有效。

## 背景

Nexus 当前本地身份基线可以初始化和登录，但与家族产品的身份体验尚未对齐。Radish 是基于 OpenIddict 的身份提供方；RadishMind 保留本地用户、会话、权限和工作区成员关系，通过 OIDC 接入 Radish。Nexus 采用后一种职责划分，支持家族登录，同时保持自部署核心路径独立。

现有 Session 与 Workspace resolver 强制关联包含密码的 `local_accounts`。只增加登录按钮会让无本地密码的 OIDC 用户无法使用，因此必须先分离账户状态、密码凭证与外部身份。

参考上游 [Radish 身份语义](https://github.com/laugh0608/Radish/blob/dev/Docs/architecture/user-identity-semantics.md)与 [RadishMind 联合登录](https://github.com/laugh0608/RadishMind/blob/dev/docs/features/admin-control-plane/local-account-radish-oidc-federated-login-v1.md)。本次依据本地源文件进行只读对照；链接用于项目定位，不宣称远程 dev 与本地完全同步。RadishMind 的确定性测试与真实 Radish 联调有独立完成线，不作为 Nexus 的验收证据。

## 决定

### 账户与授权主体

- `users.id` 继续作为 Nexus 稳定业务用户 ID；历史作者、EntityLink、权限、事件与工作区关联不变。
- 新增 `user_accounts`，以 `user_id` 唯一关联用户，独立维护 `active / disabled` 状态；密码锁定仅影响密码认证，账户禁用影响所有认证方式和会话。
- 本地密码凭证仍保存 Argon2id verifier、失败次数、锁定与凭证时间；不降低密码算法或复制 RadishMind 的开发测试认证模式。
- 外部身份以 exact `(issuer, subject)` 唯一映射本地 `user_id`；issuer 不做大小写、尾斜杠或路径折叠，subject 不从邮箱、名称或角色派生。
- 上游邮箱、展示名和角色不承担本地身份键、账户合并或授权职责。Nexus 不同步 Radish 数据库，也不成为新的 OIDC issuer。
- 两种登录最终签发相同的 Nexus opaque Session；每次请求重读账户状态及当前 membership，原有对象授权规则不变。

### 本地登录与展示

- 正式本地登录改为 `email + password`；邮箱是私有凭证标识，展示名独立填写，允许重名，继续使用既有稳定用户 ID 消歧。
- 首批支持 ASCII 邮箱，整体裁剪外侧空白、转为小写，长度不超过 254；local part 不超过 64，不允许空白、连续点和首尾点，域名使用合法 DNS label。暂不支持 SMTPUTF8、带引号 local part 或 IP literal。
- 规范化邮箱在密码凭证表唯一，不因账户禁用而回收；不能通过修改大小写产生第二个账户。
- 邮件投递与验证服务未接入前，不宣称邮箱已验证，不依赖邮箱证明外部身份归属，也不开放基于邮箱的恢复或自动合并。
- 账户准入默认关闭公开注册。首批通过管理员建立本地账户或发出一次性邀请承接团队进入；Workspace owner 只能管理其 active membership 所属 Workspace，不能授予 restricted Project 或 Environment 权限。
- 不照搬社区公开索引、靓号、展示名改名配额等与当前团队协作无关的机制。

### OIDC 客户端边界

- 首批仅配置一个可选 Radish provider。配置缺失时正常提供本地登录；配置不完整时启动失败，不悄悄隐藏配置错误。
- 使用 Authorization Code + PKCE S256，服务端换码；不使用 implicit、密码换 token 或浏览器 token 存储。
- issuer、client ID、redirect URI、authorization / token / JWKS endpoint 与签名算法采用实例配置和严格 discovery 验证；浏览器不能指定 issuer、endpoint、scope 或 redirect URI。
- 首批 scope 固定 `openid profile`，不申请 `offline_access`，不保存 access token、ID token 或 refresh token；验证后仅保留稳定身份映射。
- provider endpoint 必须为配置 issuer 同源 HTTPS，不跟随重定向；请求有超时、响应大小和并发上限。自部署允许管理员明确配置的私有网络 issuer，不能接受用户任意 URL。
- client secret 只从显式 secret 文件读取；不写入普通配置响应、日志、命令参数或备份。
- 验证 exact issuer、client audience、多 audience 时的 `azp`、签名与算法、`exp / iat / nbf`、nonce、state 与 PKCE；不启用跳过签名、issuer 或过期检查的选项。
- state、nonce 和 verifier 为随机值，事务有效期 5 分钟，一次性消费；callback 畸形、取消、超时、重放或 provider 不可用时均不给出 Session。
- state 不能单独替代浏览器绑定。独立临时 `__Host-radishnexus-oidc` HttpOnly、Secure、SameSite=Lax Cookie 绑定授权事务，允许顶层跨站 GET 回调；正常 Session Cookie 继续 SameSite=Strict。
- 授权事务持久化 state / browser token / nonce digest；PKCE verifier 使用服务器临时 Cookie 中随机材料与事务随机值派生，数据库不保存可直接使用的 verifier。恢复时事务数据不保留。
- callback 不渲染应用或第三方资产，响应 `no-store`、`Referrer-Policy: no-referrer`，以 303 跳转到固定同源完成页，再由完成页进入应用；错误只传递固定失败分类，不回显 code、state、provider error 或 claims。
- 登录只结束本地 Session；不宣称同时登出 Radish。上游单点登出与 provider 会话撤销另行定义。

### 首登、绑定与解绑

- 已绑定身份登录须重新检查 Nexus 账户状态，不由上游 claim 恢复禁用账户。
- 未绑定身份默认拒绝；只有有效的一次性邀请允许创建 Nexus 用户、账户、external binding 与 member membership。邀请、绑定和会话创建使用同一事务，保证并发单胜者。
- 同邮箱已存在本地账户时不自动合并。用户必须先登录既有账户，再显式绑定 Radish。
- 绑定从当前已验证会话发起，需要最近 10 分钟内认证，并记录发起 Session 的 digest 与 user ID；callback 重新检查该会话仍 active、近期认证且属于原账户。不能仅信任 callback Cookie 中的用户 ID。
- 解绑需要同源 CSRF、近期认证和归属检查，不能移除最后一种有效登录方式；解绑同时撤销该账户已有 Session，防止旧外部认证会话继续存活。
- 默认每个账户最多一个 Radish binding；并发绑定、解绑及密码修改按账户行加锁串行化。

### 公共接口与 Web

| 接口 | 契约 |
| --- | --- |
| `POST /api/v1/auth/sessions` | 请求改为 `{email, password}`；旧 `login_name` 不再接受，成功仍返回现有 Session DTO |
| `GET /api/v1/auth/methods` | 返回本地登录可用、Radish 是否配置和公开注册关闭；不输出 issuer / client secret |
| `POST /api/v1/auth/oidc/start` | 同源请求发起登录或在当前近期会话下发起绑定，返回受控授权 URL；临时 Cookie 绑定浏览器 |
| `GET /api/v1/auth/oidc/callback` | 一次性 GET code flow 回调，成功后签发 Nexus Session 或完成绑定，303 到固定完成页 |
| `GET /api/v1/auth/account` | 当前账户受控投影：展示名、本地登录方式是否存在、是否已绑定 Radish；不返回 issuer / subject / verifier |
| `DELETE /api/v1/auth/external-identity` | CSRF、近期认证、归属与最后登录方式保护；成功后清除当前 Cookie |
| `POST /api/v1/workspaces/{workspace_id}/invitations` | 当前 owner 创建有期限的一次性准入；不赋予 owner、Project 或 Environment 权限 |
| `POST /api/v1/auth/invitations/accept` | 同源邀请兑换，显式展示名、邮箱、密码，原子创建或加入；不按邮箱把调用方登录为已有用户 |

邀请原 token 仅向创建者返回一次，服务端存 digest；前端不持久化到 Web Storage，不自动发送外部消息。用户需要显式传递邀请，邀请码不作为 URL query，OIDC 邀请通过同源 start body 绑定事务。邀请限 24 小时、单次使用，兑换时检查邀请者仍是 active owner。已登录用户接受邀请只能加入自身账户；未登录用户不能借邮箱匹配加入已有账户。

页面提供邮箱登录、按配置显示 Radish 登录、账户登录方式及成员邀请入口。用户界面不展示协议实现细节，不以隐藏按钮替代权限判断。

### 旧账户与升级

1. 新增 forward-only migration 008，建立账户状态和外部绑定相关表；迁移既有本地账户状态，保留原 user ID、密码 verifier 和业务引用。
2. 既有 `login_name` 只作为受控迁移期间的私有历史映射材料，不能继续用于正式登录；新建账户不写入历史登录名。
3. 不猜测邮箱、不拼接假域名、不按展示名生成邮箱。管理员通过显式运维命令从标准输入提交 `user_id -> email` 映射，事务中验证来源、规范化、唯一性和未完成映射状态；成功仅输出 ID 与数量。
4. 尚未映射的旧账户不能通过邮箱密码登录，但密码 verifier 和禁用状态保留；运维映射可分批恢复登录能力。升级前必须准备并检查全部仍需本地登录的账户映射。
5. migration 吊销既有 Session；所有用户升级后重新登录。旧 Web 与新服务端不可混用，Go / Web / schema 按同一版本部署。
6. 无密码的既有业务用户不因迁移获得登录能力；显式绑定或准入不得仅按用户 ID 猜测其归属。
7. 不在本任务操作真实实例或删除数据库。集成测试使用隔离数据库验证旧 schema 数据升级、重复邮箱冲突、禁用保持和业务引用保持。
8. 回退使用升级前受控备份恢复至全新目标并部署旧 Go / Web；不得运行旧二进制写入新 schema，也不把 destructive down migration 当作正常回退。

### 备份与日志

- `user_accounts`、本地密码凭证、external binding 属于受控实例身份数据，纳入 PostgreSQL 运维备份，仍按敏感资产保护。
- Session、OIDC 授权事务和未兑换邀请只备份 schema；恢复实例不恢复旧登录态、未完成授权或邀请能力。
- provider client secret / 配置文件不进入数据库备份；恢复 OIDC 访问需要运维者重新配置原 exact issuer 和已审阅 client。
- 当前最小身份安全记录保存动作、结果、稳定用户 / Workspace / binding ID 和时间，不保存密码、邮箱、code、state、token、Cookie 或 raw claim。它与业务 Activity 分开，不宣称完成全产品 Audit。

## 实现顺序与验收

1. 账户 / 凭证拆分、邮箱 bootstrap、旧账户映射命令和 Session resolver；测试密码锁定、账户禁用、升级与恢复。
2. 本地管理员准入与邀请闭环；测试并发兑换、错误账号、撤权、过期、重复邮箱和跨 Workspace 越权。
3. OIDC transport、外部绑定与 Web；使用仓库自有 HTTPS issuer 覆盖签名、PKCE、nonce、state、时间、audience、取消、重放和 provider 失效。
4. Go race / vet、真实 PostgreSQL、备份恢复、Web 严格类型与交互测试、浏览器关键路径分别验收。
5. 真实 Radish 联调需 reviewed client registration、exact issuer / redirect URI、签名策略、secret 注入和测试账户；配置或上游远程写入不由本 ADR 自动授权，未联调前不声明 Radish 真实登录通过。

## 依赖候选与替代方案

OIDC 签名、JWKS 与 token exchange 采用维护中的标准实现，避免在业务仓库重新实现 JOSE。候选直接依赖为 `github.com/coreos/go-oidc/v3 v3.21.0`（Apache-2.0）和 `golang.org/x/oauth2 v0.36.0`（BSD-3-Clause）；go-oidc 的固定依赖包含 `github.com/go-jose/go-jose/v4 v4.1.4`（Apache-2.0）。[版本与模块图](https://github.com/coreos/go-oidc/blob/v3.21.0/go.mod)、[OIDC 验证 API](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc)和 [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation)已核对；nonce 与 azp 等应用检查仍需 Nexus 完成。

项目所有者已明确本轮不启动 Radish OIDC；上述版本仅为候选，不作为未来安装授权。本轮不修改模块图。未来启动时重新核对版本、维护与供应链并确认范围，批准后仅修改 `server/go.mod / go.sum` 及对应依赖检查合同，固定版本与 checksum，不自动升级其余依赖或 Go toolchain。安装前检查许可证与漏洞信息，出现新风险重新选择版本。自写 JWT / JWKS 或复制兄弟项目整套认证代码增加维护和审计成本，不采用；完全依赖 Radish 会破坏独立自部署与应急访问，不采用。

## 后果

Nexus 保持独立用户与权限，同时具备家族登录接入边界。成本是受控邮箱迁移、OIDC 状态存储和身份安全流程；不会因为 OIDC 接入而免除邀请、凭证恢复、审计和生产部署治理。本文记录已经确认的目标与合同，不代表所有接口已实现或真实 Radish 已验收；完成事实以当前状态和验证记录为准。
