package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/sijms/go-ora/v2"
)

type Config struct {
	Driver     string `json:"driver"`
	DataSource string `json:"datasource"`
	Select     string `json:"select"`
}

func main() {

	data := `[
		{
			"driver": "oracle",
			"datasource": "oracle://hogeuser:passw0rd@192.168.0.8:1521/pdb01",
			"select": "select * from tb_sample"
		},
		{
			"driver": "pgx",
			"datasource": "postgres://steem:steem123@192.168.0.8:5432/hivedb",
			"select": "select * from hive_state"
		}
	]`

	var configs []Config

	err := json.Unmarshal([]byte(data), &configs)
	if err != nil {
		fmt.Println("JSONエラー:", err)
		return
	}

	if len(configs) == 0 {
		fmt.Println("設定がありません")
		return
	}

	var wg sync.WaitGroup

	for _, config := range configs {

		wg.Add(1)

		go func(config Config) {
			defer wg.Done()

			fmt.Println(config.Driver)
			fmt.Println(config.DataSource)
			fmt.Println(config.Select)

			query(config)

		}(config)
	}

	wg.Wait()

	fmt.Println("すべて終了")
}

func query(config Config) {
	defer fmt.Println("*** 終了 ***")

	fmt.Println("*** 開始 ***")

	db, err := sql.Open(config.Driver, config.DataSource)
	if err != nil {
		fmt.Println("DBオープンエラー:", err)
		return
	}
	defer db.Close()

	// DBへの接続確認
	err = db.Ping()
	if err != nil {
		fmt.Println("DB接続エラー:", err)
		return
	}

	rows, err := db.Query(config.Select)
	if err != nil {
		fmt.Println("SQL実行エラー:", err)
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		fmt.Println("カラム取得エラー:", err)
		return
	}

	values := make([]any, len(columns))
	ptrs := make([]any, len(columns))

	// ヘッダー
	for _, column := range columns {
		fmt.Printf("%s\t", column)
	}
	fmt.Println()

	// データ
	for rows.Next() {

		// ポインター取得
		for i := range values {
			ptrs[i] = &values[i]
		}

		err := rows.Scan(ptrs...)
		if err != nil {
			fmt.Println("Scanエラー:", err)
			return
		}

		for _, value := range values {
			fmt.Printf("%v\t", value)
		}
		fmt.Println()
	}

	if err := rows.Err(); err != nil {
		fmt.Println("Rowsエラー:", err)
	}
}
