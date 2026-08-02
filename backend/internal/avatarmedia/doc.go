// Package avatarmedia 提供与业务主体无关的头像对象、校验、inventory 与 manifest primitives。
//
// customer 与未来的 account-profile 各自拥有 pointer／GC／maintenance；
// 本包不承载业务仓储，也不强制 customer 式 PNG 归一化。
package avatarmedia
