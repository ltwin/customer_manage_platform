// 用 Go 校验时区名：是否在 time/tzdata 嵌入的数据里（与 $GOROOT/lib/time/zoneinfo.zip 同源），
// 以及 time.LoadLocation 能否加载。
// 用法：go run main.go <tz-list.txt>...
package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	_ "time/tzdata"
)

func main() {
	zipPath := filepath.Join(runtime.GOROOT(), "lib", "time", "zoneinfo.zip")
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		fmt.Println("open zoneinfo.zip:", err)
		os.Exit(1)
	}
	defer zr.Close()
	inZip := map[string]bool{}
	for _, f := range zr.File {
		inZip[f.Name] = true
	}
	fmt.Printf("go %s, zoneinfo.zip entries: %d\n", runtime.Version(), len(inZip))
	for _, p := range os.Args[1:] {
		f, err := os.Open(p)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		total, missingZip, loadFail := 0, []string{}, []string{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			tz := strings.TrimSpace(sc.Text())
			if tz == "" {
				continue
			}
			total++
			if !inZip[tz] {
				missingZip = append(missingZip, tz)
			}
			if _, err := time.LoadLocation(tz); err != nil {
				loadFail = append(loadFail, tz)
			}
		}
		f.Close()
		fmt.Printf("%s: %d names, not in embedded tzdata: %d %v, LoadLocation failed: %d %v\n", filepath.Base(p), total, len(missingZip), missingZip, len(loadFail), loadFail)
	}
}
