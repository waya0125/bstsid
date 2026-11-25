// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package table

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteMirakurun は Mirakurun の channels.yml（BS の分）を書く。1 つの TS を 1 項目にする。
func (t *Table) WriteMirakurun(w io.Writer) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "# bstsid で生成（NIT version %d、%s）\n", t.NITVersion, t.GeneratedAt.Format("2006-01-02 15:04:05 -0700"))
	for _, e := range t.BS {
		fmt.Fprintf(bw, "- name: %s\n  type: BS\n  channel: %s\n", yamlString(e.Name()), e.Channel)
	}
	return bw.Flush()
}

// yamlString は YAML の値として安全な文字列にする（必要なら二重引用符で囲む）。
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	plain := true
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@` ") || strings.ContainsAny(s, ":#\t\n") || strings.HasSuffix(s, " ") {
		plain = false
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		plain = false
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		plain = false
	}
	if plain {
		return s
	}
	return strconv.Quote(s)
}

// WriteDVBv5 は dvbv5 のチャンネル設定（recisdb の dvbv5_channels_isdbs.conf と同じ形）を書く。
// FREQUENCY は第一 IF（kHz）、STREAM_ID は TSID（10 進）。
func (t *Table) WriteDVBv5(w io.Writer) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "# bstsid で生成（NIT version %d、%s）\n", t.NITVersion, t.GeneratedAt.Format("2006-01-02 15:04:05 -0700"))
	for _, e := range t.BS {
		fmt.Fprintf(bw, "[%s]\n\tDELIVERY_SYSTEM = ISDBS\n\tFREQUENCY = %d\n\tSTREAM_ID = %d\n\n", e.Channel, e.IFKHz(), e.TSID)
	}
	return bw.Flush()
}

// CH2Options は TVTest の .ch2 を書くときの、BonDriver のチューニング空間・チャンネル番号の割り当て。
type CH2Options struct {
	// Space は BS のチューニング空間の番号（BonDriver_Friio は 1）。
	Space int
	// SlotsPerTransponder は 1 トランスポンダーあたりのチャンネル番号の数。
	// BonDriver のチャンネル番号 = (tp-1)/2 * SlotsPerTransponder + slot（BonDriver_Friio は 4）。
	SlotsPerTransponder int
	// AllServices が false なら デジタル TV（service_type 0x01）だけ書く。
	AllServices bool
}

// WriteCH2 は TVTest のチャンネル設定ファイル（.ch2）の BS の分を書く。文字コードは BOM 付き UTF-8。
// 1 行が 1 サービス: 名前,チューニング空間,チャンネル,リモコン番号,サービスタイプ,サービスID,ネットワークID,TSID,状態
func (t *Table) WriteCH2(w io.Writer, opt CH2Options) error {
	if opt.SlotsPerTransponder <= 0 {
		return fmt.Errorf("SlotsPerTransponder は 1 以上")
	}
	bw := bufio.NewWriter(w)
	bw.WriteString("\ufeff")
	fmt.Fprintf(bw, "; TVTest チャンネル設定ファイル（bstsid で生成、NIT version %d）\n", t.NITVersion)
	fmt.Fprintf(bw, ";#SPACE(%d,BS)\n", opt.Space)
	for _, e := range t.BS {
		if e.Slot >= opt.SlotsPerTransponder {
			bw.Flush()
			return fmt.Errorf("%s: スロット %d が 1 トランスポンダーあたりの数 %d に収まらない", e.Channel, e.Slot, opt.SlotsPerTransponder)
		}
		ch := (e.Transponder-1)/2*opt.SlotsPerTransponder + e.Slot
		for _, s := range e.Services {
			if !opt.AllServices && s.Type != 0x01 {
				continue
			}
			name := s.Name
			if name == "" {
				name = fmt.Sprintf("%s %d", e.Channel, s.ServiceID)
			}
			name = strings.ReplaceAll(name, ",", "、")
			fmt.Fprintf(bw, "%s,%d,%d,0,%d,%d,%d,%d,1\n", name, opt.Space, ch, s.Type, s.ServiceID, t.NetworkID, e.TSID)
		}
	}
	return bw.Flush()
}
