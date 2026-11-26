# bstsid

BS の TS に流れている NIT（PID 0x0010）と SDT（PID 0x0011）から、BS 全局の
**TSID ⇔ トランスポンダー（BSnn_s）表** を作るツールです。

BS は局の再編で TSID の割り当てが変わります（例: BS09・BS23 で相対 TS 番号が飛んでいる）。
手で表を直す代わりに、どれか 1 つの BS チャンネルを数十秒受けて表を作り直せるようにします。

作った表（JSON）から次の形式も書けます。

| 形式 | 用途 |
|---|---|
| `mirakurun` | Mirakurun の `channels.yml`（BS の分） |
| `dvbv5` | recisdb などが読む `dvbv5_channels_isdbs.conf` と同じ形 |
| `ch2` | TVTest のチャンネル設定ファイル（BonDriver_Friio のチャンネル番号の割り当て） |

Go の録画コマンド（recfriio・recgvmvp など）からは `table` パッケージを import すれば表を読めます。

## 使い方

```sh
# どれか 1 つの BS チャンネルを 20 秒ほど受けて、表を作る（そろった時点で止まる）
recfriio --b25 BS15_0 20 - | bstsid scan -o bs.json

# 一覧を見る
bstsid show bs.json

# Mirakurun の channels.yml（BS の分）を書く
bstsid export -f mirakurun -o channels-bs.yml bs.json

# recisdb 形式
bstsid export -f dvbv5 -o dvbv5_channels_isdbs.conf bs.json

# TVTest の .ch2（BonDriver_Friio: BS はチューニング空間 1、1 トランスポンダーにつき 4 チャンネル）
bstsid export -f ch2 -ch2-space 1 -ch2-slots 4 -o BonDriver_Friio.ch2 bs.json

# 前回の表との違いを見る（違いがあれば終了コード 1）
bstsid diff bs-old.json bs.json
```

`scan` は NIT と、NIT に載っている全 TS の SDT（局名）がそろった時点で読むのをやめます。
NIT・SDT は 10 秒ほどの周期で送られているので、15〜30 秒分あれば足ります。
局名が要らなければ `-no-sdt` で NIT だけ待ちます。`-timeout 秒` で打ち切りもできます。

スクランブルは NIT・SDT には掛かっていないので、B-CAS で復号していない TS でも作れます。

## 表の形（JSON）

```json
{
  "format": 1,
  "generated_at": "2026-11-26T07:00:00+09:00",
  "network_id": 4,
  "network_name": "BSデジタル",
  "nit_version": 12,
  "bs": [
    {
      "channel": "BS01_0",
      "transponder": 1,
      "slot": 0,
      "tsid": 16400,
      "freq_khz": 11727480,
      "services": [{ "sid": 151, "type": 1, "name": "BS朝日1" }]
    }
  ]
}
```

- `channel` / `slot` は Mirakurun・recisdb・recpt1 と同じ読み方です。`slot` はそのトランスポンダーの
  TMCC の TSID 一覧（空きを除く）の何番目か、です。
- `tsid` は JSON に 16 進が無いので 10 進です（16400 = 0x4010）。
- `services[].type` は service_type（1 = デジタル TV、2 = デジタル音声、192 = データ）。
- 局名の全角英数字は半角に寄せています（`ＢＳ朝日１` → `BS朝日1`）。外字・追加記号は `〓` になります。

## 表の作り方

1. NIT（自ネットワーク、table_id 0x40、network_id 4）の TS ループから、BS の TSID と
   衛星分配システム記述子の周波数を取ります。
2. 周波数からトランスポンダー番号を決めます（11727.48 MHz + (n-1)/2 × 38.36 MHz）。
   記述子が無ければ TSID のビット（b8〜b4）で決めます。
3. 同じトランスポンダーの TS を、相対 TS 番号（TSID の下位 3 ビット）の小さい順に並べ、
   その順番を `slot` にします。TMCC の TSID 一覧はこの順なので、`BSnn_s` の `s` と一致します。
4. SDT（table_id 0x42・0x46）から局名を取ります。SDT がそろわなかった TS は、NIT の
   サービスリスト記述子からサービス ID だけ入れます。

3 の並べ方は、recisdb 1.3.2 同梱の `dvbv5_channels_isdbs.conf`（2026-11 時点）の BS 31 個の
並びを再現できることをテストで確かめています（`scan/scan_test.go`）。

## Go から使う

```go
import "github.com/waya0125/bstsid/table"

t, err := table.Load("/etc/bstsid/bs.json")
e, ok := t.ByChannel("BS15_0") // e.TSID, e.FreqKHz, e.Services
```

`table` パッケージは標準ライブラリだけを使います。

## ビルド

```sh
go build ./cmd/bstsid
GOOS=windows GOARCH=amd64 go build ./cmd/bstsid   # Windows 用
```

Go 1.24 以上。依存は `golang.org/x/text`（ARIB の漢字を JIS X 0208 から直すため）だけです。

## 実機での確認（2026-11-26）

MonsterTV U3S（recfriio）で BS15_0 を受けて表を作りました（NIT version 21、BS の TS 26 個、約 10 秒）。

- 26 チャンネルすべてで、表の `tsid`・recfriio が TMCC から選んだ TSID・PAT の TSID が一致
- `export -f dvbv5` の出力が、recisdb 形式の `dvbv5_channels_isdbs.conf` の BS 部分と（コメントと空行を除いて）完全に一致
- SDT（他 TS）で全局の局名がそろった

`export -f mirakurun` は TS ごとに 1 行で、`serviceId` や `isDisabled` は書きません。今ある channels.yml の BS 部分を置き換えるときは差分を見てください。

## 確かめていないこと


- `ch2` 形式は TVTest 0.10.0 でまだ読み込みを試していません。5 列目にサービスタイプ、
  文字コードは BOM 付き UTF-8 で書いています。
- 110 度 CS には対応していません（BS だけ）。

## ライセンス

MIT
