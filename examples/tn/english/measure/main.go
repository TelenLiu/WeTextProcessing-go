package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	en "github.com/TelenLiu/WeTextProcessing-go/tn/english"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "english", "measure")
	os.MkdirAll(cacheDir, 0755)

	n := en.NewNormalizer(cacheDir, true)

	testCases := []string{
		"1kg",
		"10km/h",
	}

	log.Info("=== Measure 规则示例 ===")
	for _, input := range testCases {
		start := time.Now()
		output := n.Normalize(input)
		log.Infof("输入: %q\n输出: %q (%v)\n\n", input, output, time.Since(start))
	}
}