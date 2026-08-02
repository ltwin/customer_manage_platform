// Package accountprofile 维护当前经营账号的私有资料与头像生命周期。
//
// 不拥有登录邮箱、密码、refresh session、业务 Settings 或客户资料。
// 头像对象经 avatarmedia 共享 decode（原始合规字节），禁止套用客户 PNG≤512 归一化。
package accountprofile
