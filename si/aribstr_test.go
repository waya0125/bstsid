// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package si

import (
	"testing"

	"golang.org/x/text/encoding/japanese"
)

// kanji は s を JIS X 0208 の 2 バイト符号（GL 側、0x21〜0x7e）にする。
func kanji(t *testing.T, s string) []byte {
	t.Helper()
	b, err := japanese.EUCJP.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] &= 0x7f
	}
	return b
}

func TestDecodeStringKanji(t *testing.T) {
	if got := DecodeString(kanji(t, "ＢＳ朝日１")); got != "BS朝日1" {
		t.Errorf("got %q", got)
	}
}

func TestDecodeStringMixed(t *testing.T) {
	// LS1 で英数を GL に呼んで "NHK"、LS0 で漢字に戻して "ＢＳ"、GR（平仮名）で "あ"、
	// ESC 0x7c（LS3R）で GR を片仮名にして "ア"
	b := []byte{0x0e, 'N', 'H', 'K', 0x0f}
	b = append(b, kanji(t, "ＢＳ")...)
	b = append(b, 0xa2)             // 平仮名 0x22 = あ
	b = append(b, 0x1b, 0x7c, 0xa2) // 片仮名 0x22 = ア
	b = append(b, 0xf7)             // 片仮名 0x77 = ヽ
	if got := DecodeString(b); got != "NHKBSあアヽ" {
		t.Errorf("got %q", got)
	}
}

func TestDecodeStringControlsSkipped(t *testing.T) {
	// MSZ（0x89）・NSZ（0x8a）・COL（0x90 0x48）は読み飛ばす。SS2 で G2（平仮名）を 1 文字だけ
	b := []byte{0x89, 0x90, 0x48, 0x0e, 'A', 0x8a, 0x19, 0x22, 'B'}
	if got := DecodeString(b); got != "AあB" {
		t.Errorf("got %q", got)
	}
}

func TestDecodeStringDesignate(t *testing.T) {
	// ESC 0x28 0x49 で G0 を JIS X 0201 片仮名にして ｱ（0x31）
	b := []byte{0x1b, 0x28, 0x49, 0x31}
	if got := DecodeString(b); got != "ｱ" {
		t.Errorf("got %q", got)
	}
	// 追加記号（90 区以降）は〓
	if got := DecodeString([]byte{0x7a, 0x50}); got != "〓" {
		t.Errorf("got %q", got)
	}
}
