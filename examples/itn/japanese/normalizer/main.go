package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	ja_itn "github.com/TelenLiu/WeTextProcessing-go/itn/japanese"
)

func main() {
	cacheDir := filepath.Join(".cache", "itn", "japanese", "normalizer")
	os.MkdirAll(cacheDir, 0755)

	n := ja_itn.NewInverseNormalizer(cacheDir, true, false, true, true, false)

	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		// Cardinal - 数字
		{"一", "1", "单数字"},
		{"十", "10", "十"},
		{"十一", "11", "十一"},
		{"百", "100", "百"},
		{"千", "1000", "千"},
		{"一万", "10000", "一万"},
		{"マイナス百", "-100", "负数"},

		// Decimal - 小数
		{"三点一四", "3.14", "小数"},

		// Char - 字符（保持原样）
		{"東京", "東京", "地名"},
		{"A", "A", "英文字母"},

		// Fraction - 分数
		{"四分の三", "3/4", "分数"},

		// Math - 数学
		{"一プラス一", "1+1", "加法"},
		{"三マイナス二", "3-2", "减法"},

		// Measure - 単位
		{"百人", "100人", "単位（人数）"},
		{"四十四キログラム", "44kg", "単位（重量）"},

		// Date - 日期
		{"二〇二二年一月一日", "2022年1月1日", "日期"},
		{"一月一日", "1月1日", "月日"},

		// Time - 时间
		{"二時零二分", "2時02分", "时间"},
		{"一時三十分三秒", "1時30分3秒", "时分秒"},

		// Money - 货币
		{"百円", "100円", "货币"},
		{"五千円", "5000円", "货币（五千）"},

		// Ordinal - 序数
		{"一番", "1番", "序数"},

		// Whitelist - 白名单（保持原样）
		{"十三湖", "十三湖", "白名单"},
	}

	fmt.Println("=== ITN Japanese InverseNormalizer 示例 ===")
	fmt.Println("模式: full_to_half=false, enable_standalone_number=true, enable_0_to_9=true")
	fmt.Println()
	for _, tc := range testCases {
		start := time.Now()
		output := n.Normalize(tc.input)
		status := "✓"
		if output != tc.expected {
			status = "✗"
		}
		fmt.Printf("[%s] %s 输入: %q\n输出: %q (期望: %q) (%v)\n\n", tc.desc, status, tc.input, output, tc.expected, time.Since(start))
	}
}