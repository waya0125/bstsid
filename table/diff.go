// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package table

import (
	"fmt"
	"sort"
	"strings"
)

// Diff は 2 つの表の違いを人が読む形で返す。違いが無ければ空。
// BSnn_s ごとに、TSID・周波数・局（サービス）の変化を並べる。
func Diff(old, new *Table) []string {
	o := map[string]Entry{}
	n := map[string]Entry{}
	var names []string
	for _, e := range old.BS {
		o[e.Channel] = e
		names = append(names, e.Channel)
	}
	for _, e := range new.BS {
		if _, ok := o[e.Channel]; !ok {
			names = append(names, e.Channel)
		}
		n[e.Channel] = e
	}
	sort.Slice(names, func(i, j int) bool {
		ti, si, _ := ParseChannel(names[i])
		tj, sj, _ := ParseChannel(names[j])
		if ti != tj {
			return ti < tj
		}
		return si < sj
	})
	var out []string
	for _, ch := range names {
		a, inOld := o[ch]
		b, inNew := n[ch]
		switch {
		case !inNew:
			out = append(out, fmt.Sprintf("- %s 0x%04x %s（無くなった）", ch, a.TSID, a.Name()))
		case !inOld:
			out = append(out, fmt.Sprintf("+ %s 0x%04x %s（増えた）", ch, b.TSID, b.Name()))
		default:
			var ch2 []string
			if a.TSID != b.TSID {
				ch2 = append(ch2, fmt.Sprintf("TSID 0x%04x → 0x%04x", a.TSID, b.TSID))
			}
			if a.FreqKHz != b.FreqKHz {
				ch2 = append(ch2, fmt.Sprintf("周波数 %d → %d kHz", a.FreqKHz, b.FreqKHz))
			}
			if sa, sb := servicesString(a), servicesString(b); sa != sb {
				ch2 = append(ch2, fmt.Sprintf("局 [%s] → [%s]", sa, sb))
			}
			if len(ch2) > 0 {
				out = append(out, fmt.Sprintf("~ %s %s", ch, strings.Join(ch2, "、")))
			}
		}
	}
	return out
}

func servicesString(e Entry) string {
	parts := make([]string, 0, len(e.Services))
	for _, s := range e.Services {
		if s.Name != "" {
			parts = append(parts, fmt.Sprintf("%d %s", s.ServiceID, s.Name))
		} else {
			parts = append(parts, fmt.Sprintf("%d", s.ServiceID))
		}
	}
	return strings.Join(parts, " / ")
}
