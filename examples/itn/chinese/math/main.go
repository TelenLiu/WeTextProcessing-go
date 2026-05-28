package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	chinese_itn "github.com/TelenLiu/WeTextProcessing-go/itn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "itn", "chinese", "math")
	os.MkdirAll(cacheDir, 0755)

	n := chinese_itn.NewInverseNormalizer(cacheDir, true, false, false, false, false)

	testCases := []string{
		"一加二",
		"一加二加三",
		"二小于等于四",
		"二大于等于一",
	}

	fmt.Println("=== ITN Math (口语转数学表达式) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		fmt.Printf("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}