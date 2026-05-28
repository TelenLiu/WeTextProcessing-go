package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	chinese_itn "github.com/TelenLiu/WeTextProcessing-go/itn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "itn", "chinese", "date")
	os.MkdirAll(cacheDir, 0755)

	n := chinese_itn.NewInverseNormalizer(cacheDir, true, false, false, false, false)

	testCases := []string{
		"二零二二年一月一日",
		"二零二二年一月",
		"一月一日",
	}

	fmt.Println("=== ITN Date (口语转日期) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		fmt.Printf("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}