main(4).go を基準にCSV更新処理だけを修正。

- 新しい週: covid19.csv に追記
- 同じ週: 重複させず更新
- 日付順に保存
- PDF保存先は従来どおり ./download/
- baseDir() は元ソースの os.Getwd() を維持

テスト:
go run . -update -week 35
cat covid19.csv
