package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/HiChen85/pal--reverse--learn/internal/apkinspect"
)

func runAPK(args []string, out, errout io.Writer) error {
	fs := flag.NewFlagSet("apk", flag.ContinueOnError)
	fs.SetOutput(errout)
	input := fs.String("file", "", "实际 APK 文件；Git LFS 指针须先下载原始对象")
	output := fs.String("out", "", "仅写 JSON 报告，默认 stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || *input == "" {
		return errors.New("用法: palverify apk -file <APK路径> [-out reports/apk.json]")
	}
	if *output != "" {
		if strings.ToLower(filepath.Ext(*output)) != ".json" {
			return errors.New("报告只允许写入 .json 文件")
		}
		a, err := filepath.Abs(*input)
		if err != nil {
			return err
		}
		b, err := filepath.Abs(*output)
		if err != nil {
			return err
		}
		if a == b {
			return errors.New("报告不能覆盖 APK 输入")
		}
	}
	report, err := apkinspect.Inspect(*input)
	if err != nil {
		return err
	}
	result := struct {
		Version          int               `json:"format_version"`
		Kind             string            `json:"kind"`
		FullGameVerified bool              `json:"full_game_verified"`
		Archive          apkinspect.Report `json:"archive"`
		Scope            string            `json:"scope"`
	}{Version: 1, Kind: "apk_metadata_inventory", Archive: report, Scope: "Static archive metadata only; no package installation, application execution, dialogue route verification or release identity inference."}
	if err = writeReport(*output, result, out); err != nil {
		return fmt.Errorf("保存APK报告: %w", err)
	}
	return nil
}
