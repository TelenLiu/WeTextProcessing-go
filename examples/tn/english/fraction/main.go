package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	en "github.com/TelenLiu/WeTextProcessing-go/tn/english"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "english", "fraction")
	os.MkdirAll(cacheDir, 0755)

	n := en.NewNormalizer(cacheDir, true)

	testCases := []string{
		"1/2",
		"2/3",
	}

	fmt.Println("=== Fraction 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		fmt.Printf("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}