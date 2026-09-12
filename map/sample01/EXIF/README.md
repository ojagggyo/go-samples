# 写真マップ（Goサンプル）

写真ファイル内のEXIF GPS情報を読み取り、現在の地図表示範囲にある写真だけを表示します。

## 対応範囲

- JPEG（`.jpg`、`.jpeg`）
- 写真本体にGPS情報が入っている写真
- Windows／Ubuntu

この最小サンプルは、Google Takeoutの付属JSONにだけ保存された位置情報にはまだ対応していません。

## 実行方法

Go 1.22以降を用意し、このフォルダで次を実行します。

### Windows PowerShell

```powershell
go mod download
go run . -photos "D:\GooglePhotos"
```

### Ubuntu

```bash
go mod download
go run . -photos "/home/yasu/GooglePhotos"
```

起動後、ブラウザで次を開きます。

```text
http://127.0.0.1:8080
```

終了するときは、実行中の画面で `Ctrl+C` を押します。

## EXEの作成

```powershell
go build -o photo-map.exe .
.\photo-map.exe -photos "D:\GooglePhotos"
```

地図表示にはOpenStreetMapとLeafletを使うため、地図を表示するときはインターネット接続が必要です。写真データはPC内から表示され、外部へアップロードしません。
