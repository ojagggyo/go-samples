package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

const baseURL = "http://localhost:8080"

func runTests() {

	fmt.Println()
	fmt.Println("================================")
	fmt.Println("TEST START")
	fmt.Println("================================")

	// curl http://localhost:8080/users
	fmt.Println("=== GET /users ===")
	get("/users")

	// curl -X POST -d '{"name":"Taro","age":20}' http://localhost:8080/users
	fmt.Println("=== POST /users ===")
	post("/users", `{"name":"Taro","age":20}`)

	// curl -X POST -d '{"name":"Hanako","age":25}' http://localhost:8080/users
	fmt.Println("=== POST /users ===")
	post("/users", `{"name":"Hanako","age":25}`)

	// curl -X POST -d '{"name":"Jiro","age":30}' http://localhost:8080/users
	fmt.Println("=== POST /users ===")
	post("/users", `{"name":"Jiro","age":30}`)

	// curl http://localhost:8080/users
	fmt.Println("=== GET /users ===")
	get("/users")

	// curl http://localhost:8080/users/1
	fmt.Println("=== GET /users/1 ===")
	get("/users/1")

	// curl -X PUT -d '{"name":"Taro Updated","age":21}' http://localhost:8080/users/1
	fmt.Println("=== PUT /users/1 ===")
	put("/users/1", `{"name":"Taro Updated","age":21}`)

	// curl http://localhost:8080/users/1
	fmt.Println("=== GET /users/1 ===")
	get("/users/1")

	// curl -X DELETE http://localhost:8080/users/3
	fmt.Println("=== DELETE /users/3 ===")
	deleteRequest("/users/3")

	// 削除確認
	fmt.Println("=== GET /users/3 ===")
	get("/users/3")

	// 404
	fmt.Println("=== GET /users/999 ===")
	get("/users/999")

	// 400
	fmt.Println("=== GET /users/abc ===")
	get("/users/abc")

	// 400
	fmt.Println("=== POST invalid JSON ===")
	post("/users", `{"name":"Taro","age":}`)

	// 400
	fmt.Println("=== PUT invalid JSON ===")
	put("/users/1", `{"name":"Taro","age":}`)

	fmt.Println()
	fmt.Println("================================")
	fmt.Println("TEST FINISHED")
	fmt.Println("================================")
}

func get(path string) {

	resp, err := http.Get(baseURL + path)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	printResponse(resp)
}

func post(path string, data string) {

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+path,
		bytes.NewBufferString(data),
	)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	printResponse(resp)
}

func put(path string, data string) {

	req, err := http.NewRequest(
		http.MethodPut,
		baseURL+path,
		bytes.NewBufferString(data),
	)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	printResponse(resp)
}

func deleteRequest(path string) {

	req, err := http.NewRequest(
		http.MethodDelete,
		baseURL+path,
		nil,
	)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	printResponse(resp)
}

func printResponse(resp *http.Response) {

	defer resp.Body.Close()

	fmt.Println("Status:", resp.Status)

	body, err := io.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	fmt.Println("Body:", string(body))
	fmt.Println()
}

func main() {
	runTests()
}
