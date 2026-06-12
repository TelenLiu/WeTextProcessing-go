package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	chinese_itn "github.com/TelenLiu/WeTextProcessing-go/itn/chinese"
)

func main() {
	cacheDir := filepath.Join("cache", "itn", "chinese", "money")
	os.MkdirAll(cacheDir, 0755)

	n := chinese_itn.NewInverseNormalizer(cacheDir, true, false, false, false, false)

	testCases := []string{
		"一点二五元",
		"一点二五人民币",
	}

	log.Info("=== ITN Money (口语转货币) 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		log.Infof("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}