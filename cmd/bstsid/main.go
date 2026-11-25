// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// bstsid は BS の TS の NIT・SDT から、全局の TSID⇔トランスポンダー表を作る。
//
//	recfriio BS15_0 20 - | bstsid scan > bs.json
//	bstsid export -f mirakurun bs.json > channels-bs.yml
//	bstsid diff old.json new.json
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/waya0125/bstsid/scan"
	"github.com/waya0125/bstsid/table"
)

var version = "dev"

const usage = `使い方:
  bstsid scan [-i 入力.ts] [-o 出力.json] [-timeout 秒] [-no-sdt]
      BS を選局した TS（標準入力か -i）から NIT と SDT を読み、TSID 表を JSON で書く。
      NIT と全 TS の SDT がそろった時点で読むのをやめる。15〜30 秒分の TS があれば足りる。
  bstsid export -f 形式 [-o 出力] [表.json]
      表を別の形式で書く。形式: mirakurun（channels.yml）、dvbv5（recisdb の conf）、ch2（TVTest）
  bstsid diff 旧.json 新.json
      2 つの表の違いを書く。違いがあれば終了コード 1。
  bstsid show [表.json]
      表を一覧で書く。
  bstsid version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	code := 0
	switch args[0] {
	case "scan":
		err = cmdScan(args[1:], stdin, stdout, stderr)
	case "export":
		err = cmdExport(args[1:], stdin, stdout)
	case "diff":
		code, err = cmdDiff(args[1:], stdout)
	case "show":
		err = cmdShow(args[1:], stdin, stdout)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "bstsid", version)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "bstsid: 知らないサブコマンド %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "bstsid:", err)
		return 2
	}
	return code
}

func openIn(path string, stdin io.Reader) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

// writeOut は path（空か "-" なら stdout）へ書く。ファイルへは一時ファイルに書いてから置き換える。
func writeOut(path string, stdout io.Writer, fn func(io.Writer) error) error {
	if path == "" || path == "-" {
		return fn(stdout)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func cmdScan(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("i", "", "入力の TS（省略時は標準入力）")
	out := fs.String("o", "", "出力の JSON（省略時は標準出力）")
	timeout := fs.Int("timeout", 0, "この秒数で読むのをやめる（0 なら入力が尽きるかそろうまで）")
	noSDT := fs.Bool("no-sdt", false, "SDT（局名）を待たず、NIT がそろった時点で書く")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r, done, err := openIn(*in, stdin)
	if err != nil {
		return err
	}
	defer done()
	c := scan.NewCollector(!*noSDT)
	var deadline time.Time
	if *timeout > 0 {
		deadline = time.Now().Add(time.Duration(*timeout) * time.Second)
	}
	if err := scan.Run(r, c, deadline); err != nil {
		return err
	}
	t, warnings, err := c.Table(time.Now())
	for _, w := range warnings {
		fmt.Fprintln(stderr, "bstsid: 注意:", w)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "bstsid: NIT version %d から BS の TS を %d 個読んだ\n", t.NITVersion, len(t.BS))
	return writeOut(*out, stdout, t.Write)
}

func loadTable(path string, stdin io.Reader) (*table.Table, error) {
	r, done, err := openIn(path, stdin)
	if err != nil {
		return nil, err
	}
	defer done()
	return table.Read(r)
}

func cmdExport(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("f", "", "形式: mirakurun / dvbv5 / ch2")
	out := fs.String("o", "", "出力（省略時は標準出力）")
	space := fs.Int("ch2-space", 1, "ch2: BS のチューニング空間の番号")
	slots := fs.Int("ch2-slots", 4, "ch2: 1 トランスポンダーあたりのチャンネル番号の数")
	all := fs.Bool("ch2-all", false, "ch2: デジタル TV 以外のサービスも書く")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("export: 表は 1 つだけ指定してください")
	}
	t, err := loadTable(fs.Arg(0), stdin)
	if err != nil {
		return err
	}
	var fn func(io.Writer) error
	switch strings.ToLower(*format) {
	case "mirakurun":
		fn = t.WriteMirakurun
	case "dvbv5":
		fn = t.WriteDVBv5
	case "ch2":
		fn = func(w io.Writer) error {
			return t.WriteCH2(w, table.CH2Options{Space: *space, SlotsPerTransponder: *slots, AllServices: *all})
		}
	case "":
		return fmt.Errorf("export: -f で形式を指定してください（mirakurun / dvbv5 / ch2）")
	default:
		return fmt.Errorf("export: 知らない形式 %q（mirakurun / dvbv5 / ch2）", *format)
	}
	return writeOut(*out, stdout, fn)
}

func cmdDiff(args []string, stdout io.Writer) (int, error) {
	if len(args) != 2 {
		return 2, fmt.Errorf("diff: 旧.json と 新.json を指定してください")
	}
	a, err := table.Load(args[0])
	if err != nil {
		return 2, err
	}
	b, err := table.Load(args[1])
	if err != nil {
		return 2, err
	}
	lines := table.Diff(a, b)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	if len(lines) > 0 {
		return 1, nil
	}
	return 0, nil
}

func cmdShow(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("show: 表は 1 つだけ指定してください")
	}
	path := ""
	if len(args) == 1 {
		path = args[0]
	}
	t, err := loadTable(path, stdin)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "# NIT version %d（%s）%s\n", t.NITVersion, t.GeneratedAt.Format("2006-01-02 15:04"), t.NetworkName)
	for _, e := range t.BS {
		var svcs []string
		for _, s := range e.Services {
			n := s.Name
			if n == "" {
				n = "?"
			}
			svcs = append(svcs, fmt.Sprintf("%d:%s", s.ServiceID, n))
		}
		fmt.Fprintf(stdout, "%-7s 0x%04x %8d kHz  %s\n", e.Channel, e.TSID, e.FreqKHz, strings.Join(svcs, " "))
	}
	return nil
}
