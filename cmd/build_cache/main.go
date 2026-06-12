package main

import (
	"os"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"

	_ "github.com/TelenLiu/WeTextProcessing-go/itn"
	chinese_itn "github.com/TelenLiu/WeTextProcessing-go/itn/chinese"
	japanese_itn "github.com/TelenLiu/WeTextProcessing-go/itn/japanese"
	chinese_tn "github.com/TelenLiu/WeTextProcessing-go/tn/chinese"
	english_tn "github.com/TelenLiu/WeTextProcessing-go/tn/english"
	japanese_tn "github.com/TelenLiu/WeTextProcessing-go/tn/japanese"
)

func main() {
	cacheDir := "cache"
	if len(os.Args) > 1 {
		cacheDir = os.Args[1]
	}
	concurrency := runtime.NumCPU()
	if concurrency < 2 {
		concurrency = 2
	}

	log.Infof("构建全部语言缓存 (cacheDir=%s, concurrency=%d)...\n", cacheDir, concurrency)

	start := time.Now()

	// 1. Chinese TN
	startSub := time.Now()
	log.Infof("[1/5] 中文TN ... ")
	chinese_tn.NewNormalizer(cacheDir+"/tn/zh", true, true, true, false, true, true, true)
	log.Infof("%v\n", time.Since(startSub))

	// 2. Chinese ITN
	startSub = time.Now()
	log.Infof("[2/5] 中文ITN ... ")
	chinese_itn.NewInverseNormalizer(cacheDir+"/itn/zh", true, true, true, false, true)
	log.Infof("%v\n", time.Since(startSub))

	// 3. English TN
	startSub = time.Now()
	log.Infof("[3/5] 英文TN ... ")
	english_tn.NewNormalizer(cacheDir+"/tn/en", true)
	log.Infof("%v\n", time.Since(startSub))

	// 4. Japanese TN
	startSub = time.Now()
	log.Infof("[4/5] 日文TN ... ")
	japanese_tn.NewNormalizer(cacheDir+"/tn/ja", true, true, true, true, true, true)
	log.Infof("%v\n", time.Since(startSub))

	// 5. Japanese ITN
	startSub = time.Now()
	log.Infof("[5/5] 日文ITN ... ")
	japanese_itn.NewInverseNormalizer(cacheDir+"/itn/ja", true, true, true, true, true)
	log.Infof("%v\n", time.Since(startSub))

	log.Infof("\n全部缓存构建完成，总耗时 %v\n", time.Since(start))
}