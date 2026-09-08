package main

import (
	"database/sql"
	"fmt"

	_ "github.com/sijms/go-ora/v2"
)

func main() {
	db, _ := sql.Open("oracle", "oracle://hogeuser:passw0rd@192.168.0.8:1521/pdb01")
	defer db.Close()

	rows, _ := db.Query("select * from tb_sample")
	defer rows.Close()

	columns, _ := rows.Columns()
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
		for i := range values { //インデックス0～2
			ptrs[i] = &values[i]
		}

		rows.Scan(ptrs...) //valuesに値が入り、そのポインタをptrsに設定

		for _, value := range values {
			fmt.Printf("%v\t", value)
		}
		fmt.Println()
	}
}
