package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

var users []User
var nextID = 1

func main() {
	http.HandleFunc("/users", usersHandler)
	http.HandleFunc("/users/", userHandler)

	fmt.Println("Server started: http://localhost:8080")

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println(err)
	}
}

func usersHandler(w http.ResponseWriter, r *http.Request) {

	switch r.Method {

	case http.MethodGet:
		getUsers(w)

	case http.MethodPost:
		createUser(w, r)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func userHandler(w http.ResponseWriter, r *http.Request) {

	idString := strings.TrimPrefix(r.URL.Path, "/users/")

	id, err := strconv.Atoi(idString)

	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	switch r.Method {

	case http.MethodGet:
		getUser(w, id)

	case http.MethodPut:
		updateUser(w, r, id)

	case http.MethodDelete:
		deleteUser(w, id)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func getUsers(w http.ResponseWriter) {

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(users)
}

func getUser(w http.ResponseWriter, id int) {

	for _, user := range users {

		if user.ID == id {

			w.Header().Set("Content-Type", "application/json")

			json.NewEncoder(w).Encode(user)

			return
		}
	}

	http.Error(w, "ユーザーが見つかりません", http.StatusNotFound)
}

func createUser(w http.ResponseWriter, r *http.Request) {

	var user User

	err := json.NewDecoder(r.Body).Decode(&user)

	if err != nil {
		http.Error(w, "JSONが不正です", http.StatusBadRequest)
		return
	}

	user.ID = nextID
	nextID++

	users = append(users, user)

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(user)
}

func updateUser(w http.ResponseWriter, r *http.Request, id int) {

	var newUser User

	err := json.NewDecoder(r.Body).Decode(&newUser)

	if err != nil {
		http.Error(w, "JSONが不正です", http.StatusBadRequest)
		return
	}

	for i := range users {

		if users[i].ID == id {

			users[i].Name = newUser.Name
			users[i].Age = newUser.Age

			w.Header().Set("Content-Type", "application/json")

			json.NewEncoder(w).Encode(users[i])

			return
		}
	}

	http.Error(w, "ユーザーが見つかりません", http.StatusNotFound)
}

func deleteUser(w http.ResponseWriter, id int) {

	for i, user := range users {

		if user.ID == id {

			users = append(users[:i], users[i+1:]...)

			fmt.Fprintf(w, "ユーザーID %dを削除しました", id)

			return
		}
	}

	http.Error(w, "ユーザーが見つかりません", http.StatusNotFound)
}
