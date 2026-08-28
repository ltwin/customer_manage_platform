package store

import "testing"

func TestLikeContainsPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyword string
		want    string
	}{
		{name: "plain keyword is only wrapped", keyword: "夜景", want: "%夜景%"},
		{name: "empty keyword still matches anything", keyword: "", want: "%%"},
		// 元字符必须按字面量匹配，否则「100%」会通配成「以 100 开头的一切」。
		{name: "percent is literal", keyword: "100%", want: `%100\%%`},
		{name: "underscore is literal", keyword: "a_b", want: `%a\_b%`},
		// 反斜杠先加倍，再轮到 % / _；顺序反了会把刚加的转义符再转义一次。
		{name: "backslash is doubled first", keyword: `a\b`, want: `%a\\b%`},
		{name: "backslash before percent stays paired", keyword: `a\%b`, want: `%a\\\%b%`},
		{name: "all metacharacters together", keyword: `%_\`, want: `%\%\_\\%`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := LikeContainsPattern(tt.keyword); got != tt.want {
				t.Fatalf("LikeContainsPattern(%q) = %q, want %q", tt.keyword, got, tt.want)
			}
		})
	}
}
