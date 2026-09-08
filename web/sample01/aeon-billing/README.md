# イオンカード請求情報取得（Go / Windows・Ubuntu）

暮らしのマネーサイトへ自動ログインし、請求画面から次の情報を取得します。

- 請求対象月
- お支払い日
- お支払い金額
- 取得日時・取得元URL
- `result.json`への保存
- Googleカレンダーへの終日予定の直接登録（重複防止）

## 重要

- ID・パスワードはファイルへ保存せず、環境変数を使います。
- メールのワンタイムパスワード認証が表示された場合は、ブラウザでの手動認証を最大5分待ちます。コードの取得・送信は自動化しません。
- イオン側の画面変更時は `config.json` のURL・セレクター調整が必要です。
- 利用前にイオンカード側の規約を確認し、短時間に繰り返しアクセスしないでください。

## 1. 準備

Google Chrome または Chromium と、Go 1.23以降をインストールします。

```bash
go mod download
```

Google Cloudで「Google Calendar API」を有効化し、デスクトップアプリ用OAuthクライアントのJSONを `client_secret.json` という名前で配置します。`config.example.json` を `config.json` へコピーします。

Windows（コマンドプロンプト）：

```bat
copy config.example.json config.json
set AEON_LOGIN_ID=あなたのID
set AEON_PASSWORD=あなたのパスワード
go run .
```

Windows（PowerShell）：

```powershell
Copy-Item config.example.json config.json
$env:AEON_LOGIN_ID="あなたのID"
$env:AEON_PASSWORD="あなたのパスワード"
go run .
```

Ubuntu：

```bash
cp config.example.json config.json
export AEON_LOGIN_ID='あなたのID'
export AEON_PASSWORD='あなたのパスワード'
go run .
```

## 2. 初回実行

同梱設定は `headless: false` のため、イオンの画面を表示します。メールの追加認証が出た場合は、ブラウザで「送信」を押し、メールのワンタイムパスワードを入力して認証を完了してください。ログイン後の画面に「ログアウト」が表示されると、請求取得へ自動で進みます。認証完了後も待機する場合は、画面に表示された案内を確認してください。

追加認証の待機時間は最大5分で、`timeout_seconds` の通常操作の制限とは別です。`headless: true` で追加認証が必要になった場合は、画面表示を有効にして再実行するよう案内して停止します。Access Denied は追加認証とは区別し、停止します。

Googleカレンダーへの登録時は、初回だけGoogle認証画面が開きます。許可後は `token.json` を再利用します。

```bash
go run .
```

成功すると次の処理を行います。

- `result.json`: 他プログラムから利用する請求情報
- Googleカレンダーへ「💳 イオンカード お支払い日」を直接登録
- 同じ支払日に同名予定がある場合は登録を省略

## 3. URLまたは画面項目が違う場合

ID欄は `input[name='username']` または `input#username` で検索します。確認した画面では `type` 属性が省略されているため、`input[type='text']` だけでは一致しません。独自の設定ファイルを使う場合も `username_selectors` にこれらを含めてください。

ログイン処理は、ID・パスワードが同じ画面にある形式と、ID入力後に「次へ」を押してパスワードを入力する形式に対応しています。入力欄・ボタンの表示は各段階で最大30秒待ちます。「次へ」はボタンの表示文字で探し、見つからなければ `next_selectors`（省略時は `submit_selectors`）を使用します。

ローカルの模擬画面によるログインテストは、Chromeをインストールした環境で `go test .` を実行してください。実サイトへのアクセスは行いません。

ログイン中はコマンドプロンプトに、入力欄の待機・入力確認・ボタンのクリックを順に表示します。画面が進まない場合は、最後に表示されたログで停止箇所を確認できます。ID・パスワードの値はログには表示しません。入力後はフォーカスを外して変更を通知し、入力内容が保持されていることを確認します。

現在のログインURLは `https://www.aeon.co.jp/app/` です。請求画面のURLが異なる場合、ブラウザで請求額画面を開き、そのURLを `config.json` の `billing_url` へ設定してください。

エラー調査時だけ `config.json` の `headless` を `false` にするか `-debug` を使います。`error.png` と画面本文には個人情報が含まれるため、共有時は必ず伏せてください。

## ビルド

認証POSTのbodyを調べる場合は `aeon-billing.exe -auth-only -network-log auth-network.jsonl -auth-body-log auth-post-body.jsonl` を実行します。`auth-post-body.jsonl` にはフォームの値をそのまま保存します（暗号化済み認証値・セッション情報を含む場合があります）。コンソールにはサイズと取得結果だけを表示します。記録対象はログイン先と同じオリジンの `/auth/realms/msweb/login-actions/authenticate` へのPOSTに限定します。

通信調査だけを行う場合は `aeon-billing.exe -auth-only -network-log network.jsonl` を実行します。請求取得・カレンダー登録は行いません。通信ログにはHTTP(S)のURL（クエリ・フラグメント除外）、メソッド、ステータス、リダイレクト先、User-Agentを記録します。本文・Cookie・認証ヘッダーは記録しません。ログは指定ファイルを毎回上書きします。

実行環境でビルド：

```bash
go build -o aeon-billing .
```

Windows用をUbuntuでクロスビルド：

```bash
GOOS=windows GOARCH=amd64 go build -o aeon-billing.exe .
```

Ubuntu用をWindows PowerShellでクロスビルド：

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o aeon-billing .
```

## 自動実行

請求額が通常更新される毎月23日以降に、WindowsタスクスケジューラまたはUbuntuのsystemd timer / cronから月1回実行してください。認証情報をタスク定義へ平文で直接書かず、OSの安全な資格情報管理を使用してください。
