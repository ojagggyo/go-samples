cd /d D:\GitHub\go-samples\map\sample02\EXIF_json




go run . -photos "D:\GooglePhotos\Takeout\Google フォト"

最大表示件数はデフォルト100件。-limit で1以上の整数を指定できます。
地図全体から分散して指定件数まで表示します。

go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -limit 500

初回の解析結果はユーザーのキャッシュフォルダ（Windowsでは通常 %LocalAppData%\photo-map-sample）に自動保存します。
次回はサイズと更新日時が同じ写真・JSONの解析を省略します。位置情報のないファイルも対象です。
追加・変更・削除の確認のため、毎回フォルダの走査は行います。動画はJSONの解析結果を再利用します。
写真フォルダごとに別のキャッシュを保存し、起動時に再利用件数・解析件数・所要時間を表示します。
内容を変更してもサイズと更新日時が両方同じ場合は検出できないため、必要なら次の引数で再解析してください。

go run . -photos "D:\GooglePhotos\Takeout\Google フォト" -rebuild-cache

HEIC/HEIFはマーカー、または一覧の「HEIC/HEIF画像を表示」をクリックするとプレビューします。
ブラウザ内で表示用JPEGに変換し、元の写真は変更しません。直近12件はページ内で再利用します。
初回表示時はCDNからheic-to 1.5.2を読み込むため、インターネット接続が必要です。
変換できない画像や通信失敗時にはエラーを表示します。ページ更新でプレビューキャッシュは消えます。
HEIC表示処理のテスト: node --test heic_preview.test.cjs


----------------------
HEICが表示されない