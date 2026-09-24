// 命令行示例：加载 RNNoise 动态库，对输入 WAV 做语音降噪并输出新文件。
//
// 用法：
//
//	go run ./example/denoise-wav -i noisy.wav -o clean.wav
//	go run ./example/denoise-wav -i in.wav -o out.wav -lib /opt/lib/librnnoise.so
//
// 库查找顺序：-lib 参数 > RNNOISE_LIBRARY 环境变量 > 系统默认库路径。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/example/noisego"
)

func main() {
	var (
		inPath  = flag.String("i", "", "输入 WAV 文件路径（16-bit 单声道 PCM）")
		outPath = flag.String("o", "", "输出 WAV 文件路径")
		libPath = flag.String("lib", os.Getenv("RNNOISE_LIBRARY"), "RNNoise 动态库路径（默认读环境变量 RNNOISE_LIBRARY）")
		libDirs = flag.String("libdir", "", "额外的动态库查找目录，多个用分隔符隔开（Linux/macOS 用 ':'，Windows 用 ';'）")
		maxVAD  = flag.Bool("vad", false, "处理完成后打印平均语音概率")
	)
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "错误：必须指定 -i 和 -o")
		flag.Usage()
		os.Exit(2)
	}

	if err := run(*inPath, *outPath, *libPath, *libDirs, *maxVAD); err != nil {
		fmt.Fprintf(os.Stderr, "失败：%v\n", err)
		// 加载失败时 LoadError 自带详细原因与解决办法，直接打印即可。
		os.Exit(1)
	}
}

func run(inPath, outPath, libPath, libDirs string, showVAD bool) error {
	// 1. 加载动态库。
	var searchDirs []string
	if libDirs != "" {
		searchDirs = filepath.SplitList(libDirs)
	}
	lib, err := noisego.Load(&noisego.LoadOptions{
		Path:        libPath,
		SearchPaths: searchDirs,
	})
	if err != nil {
		return err
	}
	defer func() { _ = lib.Close() }()
	fmt.Printf("已加载动态库：%s\n", lib.Path())

	// 2. 创建降噪上下文。
	dn, err := lib.NewDenoiser()
	if err != nil {
		return err
	}
	defer func() { _ = dn.Close() }()

	// 3. 读取输入 WAV。
	inFile, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("打开输入文件: %w", err)
	}
	wave, err := noisego.ReadWAV(inFile)
	_ = inFile.Close()
	if err != nil {
		return fmt.Errorf("读取 WAV: %w", err)
	}
	fmt.Printf("输入：%s（%d Hz，%.2f 秒，%d 采样）\n",
		inPath, wave.SampleRate,
		float64(len(wave.Samples))/float64(wave.SampleRate), len(wave.Samples))

	// 4. 必要时重采样到 RNNoise 要求的 48kHz。
	origRate := int(wave.SampleRate)
	pcm := wave.Samples
	resampled := origRate != noisego.SampleRate
	if resampled {
		pcm = noisego.ResampleLinear(pcm, origRate, noisego.SampleRate)
		fmt.Printf("采样率 %d Hz -> %d Hz（线性插值）\n", origRate, noisego.SampleRate)
	}

	// 5. 逐帧降噪，带进度回调（每 5% 打印一行）。
	start := time.Now()
	var vadSum float64
	var vadN int
	lastPct := -5
	err = dn.Process(pcm, func(done, total int, vadProb float32) error {
		vadSum += float64(vadProb)
		vadN++
		pct := done * 100 / total
		if pct >= lastPct+5 || done == total {
			lastPct = pct
			// 终端用 \r 原地刷新；输出被重定向（非字符设备）时退化为换行。
			term := "\r"
			if fi, statErr := os.Stdout.Stat(); statErr == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
				term = "\n"
			}
			fmt.Printf("进度：%3d%%（%d/%d 帧，VAD=%.2f）%s", pct, done, total, vadProb, term)
		}
		return nil
	})
	fmt.Println()
	if err != nil {
		if errors.Is(err, noisego.ErrCanceled) {
			return fmt.Errorf("处理被取消")
		}
		return fmt.Errorf("降噪处理: %w", err)
	}
	fmt.Printf("降噪完成，耗时 %s\n", time.Since(start).Round(time.Millisecond))
	if showVAD && vadN > 0 {
		fmt.Printf("平均语音概率：%.3f\n", vadSum/float64(vadN))
	}

	// 6. 重采样回原始采样率并写出。
	outSamples := pcm
	if resampled {
		outSamples = noisego.ResampleLinear(pcm, noisego.SampleRate, origRate)
		// 与原文件保持相同采样数。
		if n := len(wave.Samples); len(outSamples) >= n {
			outSamples = outSamples[:n]
		}
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建输出文件: %w", err)
	}
	defer outFile.Close()
	if err := noisego.WriteWAV(outFile, &noisego.WaveData{
		SampleRate: wave.SampleRate,
		Samples:    outSamples,
	}); err != nil {
		return fmt.Errorf("写出 WAV: %w", err)
	}
	fmt.Printf("输出：%s\n", outPath)
	return nil
}
