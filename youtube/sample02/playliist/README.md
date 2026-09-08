# YouTube URL からプレイリストを作成する Go サンプル

YouTube Data API v3 で、自分のアカウントにプレイリストを作り、複数の動画 URL を指定した順で追加します。動画の公開状態に応じて、追加できない URL は YouTube API がエラーとして返します。

## 事前準備

1. Google Cloud でプロジェクトを作成し、**YouTube Data API v3** を有効にする。
2. OAuth 同意画面を設定する。
3. OAuth クライアント ID を「デスクトップアプリ」として作り、クライアント ID をコピーする。

新しい Google Auth Platform の画面では、デスクトップ クライアントの JSON をダウンロードできない場合があります。その場合は `-client-id` を使います。認可後に作られる `token.json` は秘密情報なので Git に追加しません。

## 実行

依存関係を取得してから実行します。

```powershell
cd youtube/sample02
go mod tidy
go run . -client-id "Google Cloud でコピーしたクライアント ID" -title "お気に入り" -privacy private -urls-file urls.example.txt
```

URL を直接複数渡すこともできます。

```powershell
go run . -client-id "Google Cloud でコピーしたクライアント ID" -title "お気に入り" `
  -url "https://youtu.be/dQw4w9WgXcQ" `
  -url "https://www.youtube.com/watch?v=9bZkp7q19f0"
```

初回はブラウザーで Google 認可を行います。認可トークンは `token.json` に保存され、次回以降は再利用されます。作成されるプレイリストは既定で `private` です。`-privacy unlisted` または `-privacy public` で変更できます。旧 UI などで JSON を取得済みの場合は、`credentials.json` を置けば `-client-id` は不要です。

## 確認

```powershell
go test ./...
```
