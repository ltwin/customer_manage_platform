package creativemedia

import "testing"

// 事实在每一层都要区分未知与已知零；对 ffprobe 字面量而言，这条边界
// 就在解析辅助函数里划定。
func TestParseProbeDurationUnknownVersusZero(t *testing.T) {
	cases := map[string]int64{
		"":        -1,
		"N/A":     -1,
		"abc":     -1,
		"-1":      -1,
		"NaN":     -1,
		"Inf":     -1,
		"0":       0,
		"1.5":     1500,
		"2.0005":  2001, // 确定性四舍五入，不是截断
		"0.0004":  0,    // 真实为零的结果保持已知
		"10.9999": 11000,
		"1e300":   -1, // 不合理的量级保持未知，绝不溢出
	}
	for in, want := range cases {
		got, ok := parseProbeDuration(in)
		if want < 0 {
			if ok {
				t.Fatalf("%q reported known %d", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("%q = %d,%v want %d,known", in, got, ok, want)
		}
	}
}

func TestParseProbeRational(t *testing.T) {
	cases := []struct {
		in       string
		num, den int64
		ok       bool
	}{
		{"30000/1001", 30000, 1001, true},
		{"25", 25, 1, true},
		{"0/0", 0, 0, false},
		{"0", 0, 0, false},
		{"N/A", 0, 0, false},
		{"", 0, 0, false},
		{"-25/1", 0, 0, false},
		{"25/0", 0, 0, false},
		{"25/-1", 0, 0, false},
		{"25/x", 0, 0, false},
		{"x/25", 0, 0, false},
		{"1/2/3", 0, 0, false},
	}
	for _, c := range cases {
		num, den, ok := parseProbeRational(c.in)
		if ok != c.ok || ok && (num != c.num || den != c.den) {
			t.Fatalf("%q = %d/%d,%v want %d/%d,%v", c.in, num, den, ok, c.num, c.den, c.ok)
		}
	}
}
