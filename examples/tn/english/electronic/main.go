package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	en "github.com/TelenLiu/WeTextProcessing-go/tn/english"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "english", "electronic")
	os.MkdirAll(cacheDir, 0755)

	n := en.NewNormalizer(cacheDir, true)

	testCases := []string{
		"test@gmail.com",
		"www.example.com",
	}

	log.Info("=== Electronic 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		log.Infof("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}