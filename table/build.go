// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package table

import (
	"fmt"
	"sort"
)

// Stream は NIT の TS ループから取った 1 つの TS（Build の入力）。
type Stream struct {
	TSID uint16
	// FreqKHz は衛星分配システム記述子の周波数。0 なら記述子が無かった。
	FreqKHz  uint32
	Services []Service
}

// freqTolKHz は周波数からトランスポンダーを決めるときの許容差。
const freqTolKHz = 2000

// TransponderOfFreq は周波数（kHz）から BS のトランスポンダー番号を返す。どれにも当たらなければ 0。
func TransponderOfFreq(freqKHz uint32) int {
	if freqKHz+freqTolKHz < BS1FreqKHz {
		return 0
	}
	d := int(freqKHz) - BS1FreqKHz
	n := (d + BSStepKHz/2) / BSStepKHz
	if n < 0 || n > 11 {
		return 0
	}
	if off := d - n*BSStepKHz; off < -freqTolKHz || off > freqTolKHz {
		return 0
	}
	return n*2 + 1
}

// FreqOfTransponder は BS のトランスポンダー番号の中心周波数（kHz）を返す。
func FreqOfTransponder(tp int) uint32 { return uint32(BS1FreqKHz + (tp-1)/2*BSStepKHz) }

// TransponderOfTSID は BS の TSID のトランスポンダー番号のビット（b8〜b4）を返す。
// ARIB TR-B15 の BS の TSID の割り当て（b15〜b12 が 0100）でなければ 0。
func TransponderOfTSID(id uint16) int {
	if id&0xf000 != 0x4000 {
		return 0
	}
	return int(id>>4) & 0x1f
}

// RelativeTS は BS の TSID の相対 TS 番号（b2〜b0）を返す。
func RelativeTS(id uint16) int { return int(id & 0x7) }

// Build は NIT の TS ループから表の項目を作る。
//
// トランスポンダーは周波数（衛星分配システム記述子）で決め、記述子が無ければ TSID のビットで決める。
// スロット（BSnn_s の s）は、同じトランスポンダーの TS を相対 TS 番号（TSID の下位 3 ビット）の
// 小さい順に並べた順番にする。TMCC の TSID 一覧はこの順に並ぶので、Mirakurun などの読み方と一致する。
//
// warnings は表は作れたが気を付けた方がよいこと（周波数と TSID のビットの食い違いなど）。
func Build(streams []Stream) (entries []Entry, warnings []string) {
	seen := map[uint16]bool{}
	byTP := map[int][]Entry{}
	for _, s := range streams {
		if seen[s.TSID] {
			continue
		}
		seen[s.TSID] = true
		tp := 0
		if s.FreqKHz != 0 {
			tp = TransponderOfFreq(s.FreqKHz)
			if tp == 0 {
				warnings = append(warnings, fmt.Sprintf("TSID 0x%04x: 周波数 %d kHz が BS のどのトランスポンダーにも当たらないので飛ばした", s.TSID, s.FreqKHz))
				continue
			}
			if bit := TransponderOfTSID(s.TSID); bit != tp {
				warnings = append(warnings, fmt.Sprintf("TSID 0x%04x: 周波数では BS%02d、TSID のビットでは %d（周波数を採った）", s.TSID, tp, bit))
			}
		} else {
			tp = TransponderOfTSID(s.TSID)
			if tp < 1 || tp > 23 || tp%2 == 0 {
				warnings = append(warnings, fmt.Sprintf("TSID 0x%04x: 衛星分配システム記述子が無く、TSID からもトランスポンダーが分からないので飛ばした", s.TSID))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("TSID 0x%04x: 衛星分配システム記述子が無いので TSID のビットから BS%02d とした", s.TSID, tp))
		}
		freq := s.FreqKHz
		if freq == 0 {
			freq = FreqOfTransponder(tp)
		}
		byTP[tp] = append(byTP[tp], Entry{Transponder: tp, TSID: s.TSID, FreqKHz: freq, Services: s.Services})
	}
	tps := make([]int, 0, len(byTP))
	for tp := range byTP {
		tps = append(tps, tp)
	}
	sort.Ints(tps)
	for _, tp := range tps {
		es := byTP[tp]
		sort.SliceStable(es, func(i, j int) bool {
			ri, rj := RelativeTS(es[i].TSID), RelativeTS(es[j].TSID)
			if ri != rj {
				return ri < rj
			}
			return es[i].TSID < es[j].TSID
		})
		for i := range es {
			if i > 0 && RelativeTS(es[i].TSID) == RelativeTS(es[i-1].TSID) {
				warnings = append(warnings, fmt.Sprintf("BS%02d: TSID 0x%04x と 0x%04x の相対 TS 番号が同じ（並びは TSID の小さい順にした）", tp, es[i-1].TSID, es[i].TSID))
			}
			es[i].Slot = i
			es[i].Channel = ChannelName(tp, i)
			if i > 7 {
				warnings = append(warnings, fmt.Sprintf("BS%02d: TS が 8 つより多い", tp))
			}
		}
		entries = append(entries, es...)
	}
	return entries, warnings
}
