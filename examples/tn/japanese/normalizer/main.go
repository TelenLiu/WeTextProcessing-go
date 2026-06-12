package main

import (
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"

	ja "github.com/TelenLiu/WeTextProcessing-go/tn/japanese"
)

func main() {
	cacheDir := filepath.Join("cache", "tn", "japanese", "normalizer")
	os.MkdirAll(cacheDir, 0755)

	log.Info("正在构建 Japanese Normalizer FST（首次运行可能需要较长时间）...")
	buildStart := time.Now()

	// 完整模式：启用所有功能
	n := ja.NewNormalizer(cacheDir, true, true, true, true, true, true)

	log.Infof("构建完成，耗时: %v\n\n", time.Since(buildStart))

	log.Info("=== TN Japanese Normalizer 示例 ===")
	log.Info("模式: transliterate=true, remove_interjections=true, remove_puncts=true, full_to_half=true, tag_oov=true")
	log.Info()

	testCases := []struct {
		input string
		desc  string
	}{
		// Cardinal
		{"123", "基数词"},
		{"1%", "百分比"},
		// Date
		{"2022年1月1日", "日期"},
		// Time
		{"2時2分", "时间"},
		// Money
		{"1.25円", "货币(日元)"},
		// Measure
		{"1キロ", "度量(公里)"},
		{"10km/h", "速度"},
		// Fraction
		{"1/2", "分数"},
		// Math
		{"1+2", "数学表达式"},
		// Sport
		{"サッカー", "体育"},
		// Transliteration
		{"Tokyo", "音译"},
		// Whitelist
		{"tokyo", "白名单(小写)"},
		// Char
		{"A", "字符"},
		{"中", "汉字"},
	}

	for _, tc := range testCases {
		start := time.Now()

		done := make(chan struct{})
		var output string

		go func() {
			output = n.Normalize(tc.input)
			close(done)
		}()

		select {
		case <-done:
			log.Infof("[%s] 输入: %q\n输出: %q (%v)\n\n", tc.desc, tc.input, output, time.Since(start))
		case <-time.After(15 * time.Second):
			log.Infof("[%s] 输入: %q\n输出: <超时(15s)> (TIMEOUT)\n\n", tc.desc, tc.input)
		}
	}

	log.Info("==========================================")
}