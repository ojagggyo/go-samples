# COVID19 Go Service

外部URL:
https://steememory.com/covid19/

※ `chart/week.js` は使用しません。COVID19サービスは `/covid19/` 配下だけで完結します。

内部:
http://127.0.0.1:18081/

直接確認:
http://127.0.0.1:18081/
http://127.0.0.1:18081/index.html

`/covid19/` のプロキシは既存プロキシに任せます。

## ファイル

- main.go : Web API + 週次更新
- index.html : グラフ
- csvread.js : Go API呼び出し
- covid19.csv : グラフデータ
- download/ : ダウンロードしたPDF
- covid19.service : Webサービス
- covid19-update.service : 週次更新
- covid19-update.timer : 毎週木曜00:00

## Linux

pdftotextをインストール:

sudo apt install poppler-utils

ビルド:

go build -o covid19 main.go

起動:

./covid19

## 週次更新テスト

./covid19 -update -week 35

## systemd

sudo mkdir -p /opt/covid19
sudo cp -a . /opt/covid19/

sudo cp covid19.service /etc/systemd/system/
sudo cp covid19-update.service /etc/systemd/system/
sudo cp covid19-update.timer /etc/systemd/system/

sudo systemctl daemon-reload
sudo systemctl enable --now covid19
sudo systemctl enable --now covid19-update.timer

確認:

systemctl status covid19
systemctl status covid19-update.timer
systemctl list-timers covid19-update.timer

## Nginx / Proxy

location /covid19/ {
    proxy_pass http://127.0.0.1:18081/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

### 直接確認

```text
http://127.0.0.1:18081/
http://127.0.0.1:18081/index.html
http://127.0.0.1:18081/csvread.js
http://127.0.0.1:18081/api/getcsv?filename=covid19&lines=12&title=1
```

`csvread.js` もGoから配信します。`week.js` は使用しません。
