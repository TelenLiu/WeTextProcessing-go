package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	chinese "github.com/TelenLiu/WeTextProcessing-go/tn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "chinese", "cardinal")
	os.MkdirAll(cacheDir, 0755)

	n := chinese.NewNormalizer(cacheDir, true, false, false, false, false, false, false)

	testCases := []string{
		"1",
		"123",
		"1%",
		"127.0.0.1",
		"尾号1234",
		"010-12345678",
	}

	log.Info("=== Cardinal (数字) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		log.Infof("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}