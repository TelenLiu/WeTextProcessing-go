package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	chinese "github.com/TelenLiu/WeTextProcessing-go/tn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "chinese", "whitelist")
	os.MkdirAll(cacheDir, 0755)

	n := chinese.NewNormalizer(cacheDir, true, false, false, false, false, false, false)

	testCases := []string{
		// 测试白名单中的词语
	}

	fmt.Println("=== Whitelist (白名单) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		fmt.Printf("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}