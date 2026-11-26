// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package table は BS の TSID 表（bstsid scan が作る JSON）の型と、その読み書き・引き当てを持つ。
//
// rec 系のドライバーはこのパッケージだけを import すれば表を使える（外部依存なし）。
package table

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FormatVersion は JSON の形式の版。互換の無い変更をしたら上げる。
const FormatVersion = 1

// BS の周波数。トランスポンダー番号 tp（奇数 1〜23）の中心周波数は BS1FreqKHz + (tp-1)/2*BSStepKHz。
const (
	BS1FreqKHz = 11727480
	BSStepKHz  = 38360
	// LNBFreqKHz は BS・110 度 CS の LNB の局発周波数。第一 IF = 周波数 - LNBFreqKHz。
	LNBFreqKHz = 10678000
)

// Service は TS に載っているサービス。
type Service struct {
	ServiceID uint16 `json:"sid"`
	// Type は service_type（0x01 デジタル TV、0x02 デジタル音声、0xc0 データ など）。
	Type uint8  `json:"type"`
	Name string `json:"name,omitempty"`
}

// Entry は 1 つの TS（BSnn_s）。
type Entry struct {
	// Channel は "BS01_0" の形の名前。Mirakurun・recisdb・recpt1 と同じ読み方。
	Channel string `json:"channel"`
	// Transponder は BS のトランスポンダー番号（奇数 1〜23）。
	Transponder int `json:"transponder"`
	// Slot はそのトランスポンダーの何番目の TS か（TMCC の TSID 一覧から空きを除いた並び、0 始まり）。
	Slot int    `json:"slot"`
	TSID uint16 `json:"tsid"`
	// FreqKHz は衛星分配システム記述子の周波数（kHz）。
	FreqKHz  uint32    `json:"freq_khz"`
	Services []Service `json:"services,omitempty"`
}

// IFKHz は第一 IF の周波数（kHz）を返す。dvbv5 の conf などはこちらで書く。
func (e Entry) IFKHz() uint32 { return e.FreqKHz - LNBFreqKHz }

// Name は表示用の名前（最初のデジタル TV サービスの名前、無ければ最初のサービス、それも無ければ Channel）を返す。
func (e Entry) Name() string {
	for _, s := range e.Services {
		if s.Type == 0x01 && s.Name != "" {
			return s.Name
		}
	}
	for _, s := range e.Services {
		if s.Name != "" {
			return s.Name
		}
	}
	return e.Channel
}

// Table は BS の TSID 表。
type Table struct {
	Format      int       `json:"format"`
	GeneratedAt time.Time `json:"generated_at"`
	NetworkID   uint16    `json:"network_id"`
	NetworkName string    `json:"network_name,omitempty"`
	// NITVersion は表を作った NIT の version_number。
	NITVersion int     `json:"nit_version"`
	BS         []Entry `json:"bs"`
}

// ByChannel は "BS15_0" などの名前で引く。大文字小文字と、"BS1_0" のような 0 埋めの有無は問わない。
func (t *Table) ByChannel(name string) (Entry, bool) {
	tp, slot, err := ParseChannel(name)
	if err != nil {
		return Entry{}, false
	}
	for _, e := range t.BS {
		if e.Transponder == tp && e.Slot == slot {
			return e, true
		}
	}
	return Entry{}, false
}

// ByTSID は TSID で引く。
func (t *Table) ByTSID(id uint16) (Entry, bool) {
	for _, e := range t.BS {
		if e.TSID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Transponder は tp 番のトランスポンダーの TS を Slot 順に返す。
func (t *Table) Transponder(tp int) []Entry {
	var out []Entry
	for _, e := range t.BS {
		if e.Transponder == tp {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// ParseChannel は "BS15_0"・"bs1_2" をトランスポンダー番号とスロットに分ける。
func ParseChannel(name string) (tp, slot int, err error) {
	bad := func() (int, int, error) {
		return 0, 0, fmt.Errorf("BS のチャンネル名ではない: %q（例: BS15_0。トランスポンダーは奇数 1〜23、スロットは 0〜7）", name)
	}
	u := strings.ToUpper(strings.TrimSpace(name))
	rest, ok := strings.CutPrefix(u, "BS")
	if !ok {
		return bad()
	}
	a, b, ok := strings.Cut(rest, "_")
	if !ok || a == "" || b == "" {
		return bad()
	}
	tp, err1 := strconv.Atoi(a)
	slot, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || tp < 1 || tp > 23 || tp%2 == 0 || slot < 0 || slot > 7 {
		return bad()
	}
	return tp, slot, nil
}

// ChannelName はトランスポンダー番号とスロットから "BS01_0" の形の名前を作る。
func ChannelName(tp, slot int) string { return fmt.Sprintf("BS%02d_%d", tp, slot) }

// Read は JSON の表を読む。
func Read(r io.Reader) (*Table, error) {
	var t Table
	dec := json.NewDecoder(r)
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("表の JSON を読めない: %w", err)
	}
	if t.Format != FormatVersion {
		return nil, fmt.Errorf("表の形式の版 %d には対応していない（対応は %d）", t.Format, FormatVersion)
	}
	return &t, nil
}

// Load はファイルから表を読む。
func Load(path string) (*Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// Write は表を JSON（字下げあり）で書く。
func (t *Table) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(t)
}
