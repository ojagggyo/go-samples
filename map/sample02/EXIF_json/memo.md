cd /d D:\GitHub\go-samples\map\sample02\EXIF_json
go run . -photos "D:\GooglePhotos\Takeout\Google フォト"

docker exec openresty openresty -T 2>&1
sudo htpasswd -B /etc/nginx/photos.htpasswd photos
sudo htpasswd -B /etc/nginx/photos.htpasswd photos-admin
docker cp /etc/nginx/photos.htpasswd openresty:/etc/nginx/photos.htpasswd



-addr 0.0.0.0:18082
-limit 500
-rebuild-cache
-trusted-proxies 192.168.0.8
-admin-users photos-admin

go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -limit 50 -addr 0.0.0.0:18082 -trusted-proxies 192.168.0.08

最大表示件数はデフォルト100件。-limit で1以上の整数を指定できます。
地図全体から分散して指定件数まで表示します。

go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -limit 500

初回の解析結果はユーザーのキャッシュフォルダ（Windowsでは通常 %LocalAppData%\photo-map-sample）に自動保存します。
次回はサイズと更新日時が同じ写真・JSONの解析を省略します。位置情報のないファイルも対象です。
追加・変更・削除の確認のため、毎回フォルダの走査は行います。動画はJSONの解析結果を再利用します。
写真フォルダごとに別のキャッシュを保存し、起動時に再利用件数・解析件数・所要時間を表示します。
内容を変更してもサイズと更新日時が両方同じ場合は検出できないため、必要なら次の引数で再解析してください。

go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -rebuild-cache

HEIC/HEIFも右サイドの一覧で、画面に入った画像から自動でサムネイルを表示します。
マーカーまたはサムネイルをクリックするとプレビューします。拡張子がHEICでも中身がJPEG/PNGなら変換せず表示します。
ブラウザ内で表示用JPEGに変換し、元の写真は変更しません。直近12件はページ内で再利用します。
初回表示時はCDNからheic-to 1.5.2を読み込むため、インターネット接続が必要です。
変換できない画像や通信失敗時にはエラーを表示します。ページ更新でプレビューキャッシュは消えます。
HEIC表示処理のテスト: node --test heic_preview.test.cjs


----------------------
## 位置情報のない写真に場所を紐づける

1. 右サイドの「表示」を「位置情報のない写真」に切り替えます。
   「撮影年」「月」で絞り込めます。年だけ・月だけの指定や「撮影日時なし」も選べます。
   年月の判定はTakeoutの撮影日時を日本時間に換算します。絞り込みを変えても選択は保持され、表示外の選択も一括保存の対象になります。
2. 写真のチェックボックスで1枚または複数枚を選びます。「このページをすべて選択」も使え、ページ移動後も選択を保持します。「すべて解除」で選択を消せます。
3. 地図をクリックします。青い丸はドラッグで調整できます。
4. 「選択した写真にこの場所を保存」を押すと、選択した全写真に同じ座標を保存します。「地図の写真・動画」に切り替えると地図に表示されます。

Takeout JSONの photoTakenTime.timestamp がある場合、前後1時間以内で撮影日時が最も近い位置情報付き写真を候補にします。
候補には元のEXIFまたはTakeout JSONに位置情報がある写真・動画を使い、手動で紐づけた写真は使いません。
「候補の場所を地図で確認」を押して位置を確認・調整し、保存してください。日時が近くても同じ場所とは限りません。
撮影日時がない場合も地図で手動指定できます。現在、候補検索にはEXIF日時やファイル更新日時は使用しません。

元の写真・Takeout JSONは変更しません。紐づけは実行フォルダの photo-locations.json に保存し、再起動時に復元します。
このファイルはキャッシュではないため、紐づけを残すには保管してください。
別の保存先を指定する場合は -locations "保存先のJSONファイル" を使います。写真フォルダごとに別のファイルを指定してください。
複数選択時の日時候補は、最初に選択した写真を基準にします。選択した全写真に同じ場所を付けてよいか確認して保存してください。
保存に成功すると選択は解除されます。保存に失敗した場合は全写真の紐づけと選択を維持し、再試行できます。
初回のみ撮影日時取得のため既存の解析キャッシュを再構築します。

確認: go test . / node --test heic_preview.test.cjs（oldフォルダは対象外）

## 一般アカウント・管理者アカウント

OpenRestyのBasic認証と連携します。既存の photos は一般ユーザー、photos-admin は管理者です。
一般ユーザーは位置情報付きの写真・動画を閲覧できます。
位置情報なしの表示切替・一覧・画像の直接参照・位置情報の保存は管理者だけが利用できます。
管理者が位置情報を保存した写真は、従来どおり地図に追加され、一般ユーザーにも公開されます。
位置情報未登録の画像は旧URL /photo?id=-1 などからも参照できません。

導入時は、OpenResty設定とGoプログラムを一緒に更新してください。
Goプログラムは認証済みユーザー名を受け取れないアクセスを403で拒否します。
従来の http://127.0.0.1:8080/ へのブラウザ直接アクセスも対象です。

1. サーバー側の既存パスワードファイルに管理者を追加します。パスワードは対話入力します。
   既存の photos を残すため -c は付けません。

   ```sh
   sudo htpasswd -B /etc/nginx/photos.htpasswd photos-admin
   docker cp /etc/nginx/photos.htpasswd openresty:/etc/nginx/photos.htpasswd
   ```

   ファイルをコンテナへマウントしている場合は、マウント元を更新してください。
   管理者のパスワードは photos と別にします。

2. openresty-photos.conf.example を参考に、既存のHTTPS server内の /photos/ 設定を更新します。
   proxy_pass の接続先は現在の写真アプリの接続先を使い、末尾の / を残してください。
   X-Photo-User は必ず $remote_user で上書きし、Basic認証を全API・画像にも適用します。
   Hostも維持します。proxy_cache off はアカウント間のキャッシュ共有を防ぎます。

3. Goプログラムを再起動します。同一ホストのループバック接続の場合の例です。

   ```powershell
   go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -admin-users photos-admin
   ```

   -admin-users はカンマ区切りで複数名を指定できます。名前は大文字・小文字を区別します。
   指定に含まれない認証済みアカウントは一般ユーザーになります。
   管理者名の指定はアカウント作成ではありません。Basic認証側にも同じ名前が必要です。

   Dockerや別ホストのOpenRestyから接続する場合は、Goから見える実際の接続元IPを
   -trusted-proxies "192.0.2.10" のように指定してください（このIPは説明用です）。
   デフォルトは 127.0.0.1,::1 です。IPの列挙のみ対応し、X-Forwarded-Forは信用しません。
   -addr も既存の構成に合わせ、Goのポートへは認証プロキシだけが接続できる構成にします。
   信頼するIP上のプロセスはユーザー名を指定できるため、共有の転送元IPを無条件に信頼しないでください。

4. OpenRestyの設定を検証して再読み込みします。

   ```sh
   docker exec openresty openresty -t
   docker exec openresty openresty -s reload
   ```

5. https://steememory.com/photos/ を別々のブラウザプロファイルで開き、photos と photos-admin を確認します。
   Basic認証はブラウザが資格情報を保持するため、アカウントの切替には別プロファイルが確実です。
   photos では「位置情報のない写真」がなく、/photos/api/unlocated は403になります。
   photos-admin では一覧・写真の表示・位置保存を利用できます。

設定の参照元: [Nginx proxy module](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)、
[Nginx Basic認証](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html)。
