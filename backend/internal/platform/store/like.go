package store

import "strings"

// likeMetaEscaper 转义 LIKE/ILIKE 的元字符。
// PostgreSQL 的 LIKE 在省略 ESCAPE 子句时默认以反斜杠为转义符，所以反斜杠自身
// 必须先加倍，再轮到 % 与 _；顺序反了会把刚加上的转义符又转义一次。
var likeMetaEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// LikeContainsPattern 把用户输入的关键词包成 ILIKE 的「包含」模式。
//
// 关键词里的 % 和 _ 是 LIKE 元字符，不转义就会被当通配符：搜「100%」会匹配到
// 所有以 100 开头的行，搜「a_b」会匹配 a 任意字符 b。用户看不到任何报错，只会
// 拿到一批莫名其妙的结果，所以这里一律按字面量处理。
//
// 调用方直接把返回值作为参数传给 ILIKE 占位符即可，不需要写 ESCAPE 子句——
// 反斜杠正是 PostgreSQL 省略该子句时的默认转义符。
func LikeContainsPattern(keyword string) string {
	return "%" + likeMetaEscaper.Replace(keyword) + "%"
}
