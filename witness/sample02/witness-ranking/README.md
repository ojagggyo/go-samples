# witness-ranking

現在の以下の処理をGoへ集約するための最初の実装です。

- ranking.sh
- witness_ranking.sh
- miss_week.sh
- getcsv2.php

## コマンド

### ビルド

```bash
go build -o witness-ranking .
```

### データ更新

```bash
DATA_DIR=/data/php ./witness-ranking update
```

作成する主なファイル:

- ranking.csv
- ranking/ranking_YYYYMMDDHHMM.csv
- ranking_last_hour.csv
- ranking_last_2hours.csv
- ranking_last_day.csv
- ranking_last_week.csv
- ranking_last_month.csv
- ranking_last_year.csv
- ranking_hour.csv
- ranking_2hours.csv
- ranking_day.csv
- ranking_week.csv
- ranking_month.csv
- ranking_year.csv

### HTTPサーバー

```bash
DATA_DIR=/data/php WEB_DIR=./web ./witness-ranking serve
```

ブラウザ:

```text
http://server:8080/
```

API:

```text
/api/csv?filename=ranking_hour
/api/csv?filename=ranking_day
/api/csv?filename=ranking_week
```

同じGo HTTPサーバーからHTML/JavaScript/APIを配信する構成なので、
通常はブラウザ側のCORS問題を回避できます。

## 未実装

現在は意図的に以下をTODOにしています。

- detail.sh
- detail_all.sh
- 証人詳細画面用CSVの作成
- systemd service / timer
- 複数RPCへのフェイルオーバー
- ロック処理
- 古い履歴CSVの自動削除

まずは現在のランキング作成・比較・HTTP配信をGoへ移す土台です。
