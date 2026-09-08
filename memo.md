#### 開発環境構築
cd gmail_calendar
go mod init gmail_calendar
go get google.golang.org/api/gmail/v1
go get google.golang.org/api/calendar/v3
go get golang.org/x/oauth2/google


#### git checkout --orphan を使えば、現在のファイルを残して履歴を作り直せます。 管理外・無視対象のファイルも削除しません。
1. step1
git checkout --orphan fresh-main
git add -A
git diff --cached --stat
2. step2
git commit -m "Initial commit"
git branch -M main
3. step3
git fetch origin
git push --force-with-lease -u origin main