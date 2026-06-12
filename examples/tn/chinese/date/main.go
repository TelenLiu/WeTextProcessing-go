package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	chinese "github.com/TelenLiu/WeTextProcessing-go/tn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "chinese", "date")
	os.MkdirAll(cacheDir, 0755)

	n := chinese.NewNormalizer(cacheDir, true, false, false, false, false, false, false)

	testCases := []string{
		"2022-01-01",
		"2022/01/01",
		"2022.01.01",
		"01-01",
		"2022-01",
	}

	log.Info("=== Date (日期) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		log.Infof("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}