# Kakao Talk 自分宛て送信（Go）

YouTube URLを、ログイン中の本人のカカオトーク「自分とのトーク」へ送信します。

## Kakao Developersの設定

1. `yasu_app` のカカオログインをONにする。
2. REST APIキーのログインRedirect URIに、次を完全一致で登録する。

   `http://localhost:8080/oauth/callback`

3. カカオログインの同意項目で「カカオトーク・メッセージ送信」(`talk_message`)を任意同意にする。
4. REST APIキーのClient Secretが有効なら、その値も用意する。無効なら不要。

## Windows PowerShellで実行

このフォルダへ移動し、現在のPowerShellセッションだけにキーを設定します。

```powershell
$env:KAKAO_REST_API_KEY="REST APIキー"
$env:KAKAO_CLIENT_SECRET="Client Secret"
go run . -url "https://www.youtube.com/watch?v=VIDEO_ID" -message "おすすめ動画"
```

Client Secretが無効の場合は、2行目を実行しません。

初回だけブラウザが開きます。カカオログイン後、「カカオトーク・メッセージ送信」に同意してください。認証情報は次へ保存され、次回以降は再利用されます。

`%AppData%\kakao-send\kakao_token.json`

認証だけ先に済ませる場合：

```powershell
go run . -login
```

## EXEを作成

```powershell
go build -o kakao-send.exe .
```

実行例：

```powershell
.\kakao-send.exe -url "https://youtu.be/VIDEO_ID" -message "あとで見る動画"
```

## 注意

- `KAKAO_REST_API_KEY`、`KAKAO_CLIENT_SECRET`、`kakao_token.json`をGitへ登録しないでください。
- ポート8080をほかのプログラムが使っている場合は、そのプログラムを停止してから初回認証してください。
- これは本人の「自分とのトーク」専用です。友だちへの送信には別途Kakaoの追加権限が必要です。
