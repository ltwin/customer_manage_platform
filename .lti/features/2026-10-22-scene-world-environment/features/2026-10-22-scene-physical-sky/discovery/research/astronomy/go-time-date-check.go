// Go time.Date 对夏令时特殊时刻的取法（对照用，不进产品）。运行：go run go-time-date-check.go
// 结论：Go 的取法随偏移正负变化，文档说明 "not guaranteed"；服务端如需换算不能依赖它。

package main

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

func main() {
	cases := []struct {
		tz             string
		y, mo, d, h, m int
	}{
		{"America/New_York", 2026, 3, 8, 2, 30}, {"America/New_York", 2026, 11, 1, 1, 30},
		{"Australia/Sydney", 2026, 4, 5, 2, 30}, {"Australia/Sydney", 2026, 10, 4, 2, 30},
		{"Australia/Lord_Howe", 2026, 10, 4, 2, 15}, {"Australia/Lord_Howe", 2026, 4, 5, 1, 45},
		{"Pacific/Apia", 2011, 12, 30, 12, 0}, {"Asia/Shanghai", 1900, 1, 1, 0, 0}, {"Asia/Shanghai", 1988, 7, 1, 12, 0},
	}
	for _, c := range cases {
		loc, _ := time.LoadLocation(c.tz)
		t := time.Date(c.y, time.Month(c.mo), c.d, c.h, c.m, 0, 0, loc)
		fmt.Printf("%-20s %04d-%02d-%02d %02d:%02d -> %s\n", c.tz, c.y, c.mo, c.d, c.h, c.m, t.UTC().Format(time.RFC3339))
	}
}
