package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	db, err := sql.Open("pgx", "postgres://steem:steem123@192.168.0.8:5432/hivedb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query("select * from hive_state")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	columns, _ := rows.Columns()
	values := make([]any, len(columns))
	ptrs := make([]any, len(columns))

	for rows.Next() {
		for i := range values {
			ptrs[i] = &values[i]
		}

		rows.Scan(ptrs...)

		for i, column := range columns {
			fmt.Printf("%s=%v ", column, values[i])
		}
		fmt.Println()
	}
}
