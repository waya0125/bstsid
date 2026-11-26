// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package si

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

// ARIB STD-B24 第一編 第 2 部の 8 単位符号の文字集合（終端符号 F）。
const (
	setKanji         = 0x42
	setAlnum         = 0x4a
	setHiragana      = 0x30
	setKatakana      = 0x31
	setPropAlnum     = 0x36
	setPropHiragana  = 0x37
	setPropKatakana  = 0x38
	setJISKatakana   = 0x49
	setJISKanji1     = 0x39
	setJISKanji2     = 0x3a
	setAddSymbols    = 0x3b
	setDRCS          = 0x100 // DRCS（外字）。中身は分からないので〓にする
	setUnknown1Byte  = 0x101
	setUnknown2Bytes = 0x102
)

func twoByteSet(s int) bool {
	switch s {
	case setKanji, setJISKanji1, setJISKanji2, setAddSymbols, setUnknown2Bytes:
		return true
	}
	return false
}

// 平仮名・片仮名集合の 0x77〜0x7e は JIS X 0208 の 4・5 区に無い記号。
var hiraganaTail = []rune("ゝゞー。「」、・")
var katakanaTail = []rune("ヽヾー。「」、・")

// DecodeString は ARIB 8 単位符号の文字列を UTF-8 に直す。
//
// 番組表に要るほど細かくはしない。局名・ネットワーク名を読める程度で、
// 色・大きさなどの制御符号は読み飛ばし、外字（DRCS）と追加記号は〓にする。
// 全角の英数字と和文間隔は半角に寄せる（"ＢＳ朝日１" → "BS朝日1"）。
func DecodeString(b []byte) string {
	g := [4]int{setKanji, setAlnum, setHiragana, setKatakana}
	gl, gr := 0, 2
	var sb strings.Builder
	single := -1 // SS2/SS3 で次の 1 文字だけ使う G
	put := func(set int, c1, c2 byte) {
		sb.WriteString(decodeChar(set, c1&0x7f, c2&0x7f))
	}
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x0e: // LS1
			gl = 1
			i++
		case c == 0x0f: // LS0
			gl = 0
			i++
		case c == 0x19: // SS2
			single = 2
			i++
		case c == 0x1d: // SS3
			single = 3
			i++
		case c == 0x1b: // ESC
			i += escape(b[i+1:], &g, &gl, &gr) + 1
		case c == 0x20 || c == 0xa0:
			sb.WriteByte(' ')
			i++
		case c == 0x0d || c == 0x0a:
			sb.WriteByte('\n')
			i++
		case c < 0x20 || c == 0x7f || c == 0xff:
			i += c0Params(b[i:])
		case c >= 0x80 && c < 0xa0:
			i += c1Params(b[i:])
		default:
			idx := gr
			if c < 0x80 {
				idx = gl
			}
			if single >= 0 {
				idx = single
				single = -1
			}
			set := g[idx]
			if twoByteSet(set) {
				if i+1 >= len(b) {
					i = len(b)
					continue
				}
				put(set, c, b[i+1])
				i += 2
			} else {
				put(set, c, 0)
				i++
			}
		}
	}
	return normalizeWidth(sb.String())
}

// escape は ESC の後ろを読んで G の指示・呼び出しを反映し、読んだバイト数を返す。
func escape(b []byte, g *[4]int, gl, gr *int) int {
	if len(b) == 0 {
		return 0
	}
	switch b[0] {
	case 0x6e: // LS2
		*gl = 2
		return 1
	case 0x6f: // LS3
		*gl = 3
		return 1
	case 0x7e: // LS1R
		*gr = 1
		return 1
	case 0x7d: // LS2R
		*gr = 2
		return 1
	case 0x7c: // LS3R
		*gr = 3
		return 1
	case 0x28, 0x29, 0x2a, 0x2b: // 1 バイト G 集合を G0〜G3 へ
		n := int(b[0] - 0x28)
		if len(b) >= 3 && b[1] == 0x20 { // DRCS
			g[n] = setDRCS
			return 3
		}
		if len(b) >= 2 {
			g[n] = oneByteSet(b[1])
			return 2
		}
		return len(b)
	case 0x24: // 2 バイト G 集合
		if len(b) < 2 {
			return len(b)
		}
		switch b[1] {
		case 0x28, 0x29, 0x2a, 0x2b:
			n := int(b[1] - 0x28)
			if len(b) >= 4 && b[2] == 0x20 { // DRCS
				g[n] = setDRCS
				return 4
			}
			if len(b) >= 3 {
				g[n] = twoByteSetOf(b[2])
				return 3
			}
			return len(b)
		default: // ESC 0x24 F は G0 へ
			g[0] = twoByteSetOf(b[1])
			return 2
		}
	}
	return 1
}

func oneByteSet(f byte) int {
	switch f {
	case setAlnum, setHiragana, setKatakana, setPropAlnum, setPropHiragana, setPropKatakana, setJISKatakana:
		return int(f)
	}
	return setUnknown1Byte
}

func twoByteSetOf(f byte) int {
	switch f {
	case setKanji, setJISKanji1, setJISKanji2, setAddSymbols:
		return int(f)
	}
	return setUnknown2Bytes
}

// c0Params は C0 制御符号の長さ（パラメーター込み）を返す。
func c0Params(b []byte) int {
	switch b[0] {
	case 0x16: // PAPF
		return min(2, len(b))
	case 0x1c: // APS
		return min(3, len(b))
	}
	return 1
}

// c1Params は C1 制御符号の長さ（パラメーター込み）を返す。
func c1Params(b []byte) int {
	switch b[0] {
	case 0x8b: // SZX
		return min(2, len(b))
	case 0x90: // COL
		if len(b) >= 2 && b[1] == 0x20 {
			return min(3, len(b))
		}
		return min(2, len(b))
	case 0x91, 0x93, 0x94, 0x97, 0x98: // FLC POL WMM HLC RPC
		return min(2, len(b))
	case 0x9d: // TIME
		if len(b) >= 2 && b[1] == 0x20 {
			return min(3, len(b))
		}
		// TIME 0x28 F または 0x29 ... は最後の 0x40〜0x43 まで
		for i := 1; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x43 {
				return i + 1
			}
		}
		return len(b)
	case 0x9b: // CSI: 中間・終端符号 0x20 + F まで
		for i := 1; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				return i + 1
			}
		}
		return len(b)
	case 0x95: // MACRO: MACRO 0x4f で終わる
		for i := 1; i+1 < len(b); i++ {
			if b[i] == 0x95 && b[i+1] == 0x4f {
				return i + 2
			}
		}
		return len(b)
	}
	return 1
}

func decodeChar(set int, c1, c2 byte) string {
	switch set {
	case setKanji, setJISKanji1, setJISKanji2:
		if c1 >= 0x7a { // 90〜94 区は ARIB の追加記号
			return "〓"
		}
		if set == setJISKanji2 {
			return "〓"
		}
		return jis0208(c1, c2)
	case setAlnum, setPropAlnum:
		return string(rune(c1))
	case setHiragana, setPropHiragana:
		if c1 >= 0x77 {
			return string(hiraganaTail[c1-0x77])
		}
		return jis0208(0x24, c1)
	case setKatakana, setPropKatakana:
		if c1 >= 0x77 {
			return string(katakanaTail[c1-0x77])
		}
		return jis0208(0x25, c1)
	case setJISKatakana:
		if c1 <= 0x5f {
			return string(rune(0xff61 + int(c1) - 0x21))
		}
	}
	return "〓"
}

func jis0208(c1, c2 byte) string {
	out, err := japanese.EUCJP.NewDecoder().Bytes([]byte{c1 | 0x80, c2 | 0x80})
	if err != nil || len(out) == 0 {
		return "〓"
	}
	if r, _ := utf8.DecodeRune(out); r == utf8.RuneError {
		return "〓"
	}
	return string(out)
}

// normalizeWidth は全角の英数字・記号（U+FF01〜U+FF5E）と和文間隔を半角にする。
func normalizeWidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xff01 && r <= 0xff5e:
			return r - 0xff01 + 0x21
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
}
