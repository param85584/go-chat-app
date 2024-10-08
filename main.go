package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// Task represents a task with an ID, Title, Description, and Status
type Task struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending" or "completed"
}

// Message represents a chat message
type Message struct {
	Username string `json:"username"`
	Content  string `json:"content"`
}

var (
	// Task management variables
	tasks   []Task
	nextID  int = 1
	tasksMu sync.Mutex

	// Chat application variables
	clients   = make(map[*websocket.Conn]bool) // Connected clients
	broadcast = make(chan Message)             // Broadcast channel
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// Allow connections from any origin
			return true
		},
	}
)

func main() {
	// Create a new Gorilla Mux router
	router := mux.NewRouter()
	router.Use(jsonMiddleware)

	// Task management routes
	router.HandleFunc("/tasks", createTask).Methods("POST")
	router.HandleFunc("/tasks", getTasks).Methods("GET")
	router.HandleFunc("/tasks/{id}", getTask).Methods("GET")
	router.HandleFunc("/tasks/{id}", updateTask).Methods("PUT")
	router.HandleFunc("/tasks/{id}", deleteTask).Methods("DELETE")

	// WebSocket route for chat
	router.HandleFunc("/ws", handleConnections)

	// Serve static files from the "public" directory
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./public/")))

	// Start listening for incoming chat messages
	go handleMessages()

	// Start the server
	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatal("Server error: ", err)
	}
}

// Middleware to set the Content-Type header to application/json
func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set Content-Type header
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

//////////////////////
// Task API Handlers //
//////////////////////

// Create a new task (POST /tasks)
func createTask(w http.ResponseWriter, r *http.Request) {
	var task Task
	// Decode the request body into a Task struct
	err := json.NewDecoder(r.Body).Decode(&task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tasksMu.Lock()
	defer tasksMu.Unlock()

	// Assign an ID to the new task
	task.ID = nextID
	nextID++

	// Set default status if not provided
	if task.Status == "" {
		task.Status = "pending"
	}

	// Add the new task to the slice
	tasks = append(tasks, task)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

// Get all tasks (GET /tasks)
func getTasks(w http.ResponseWriter, r *http.Request) {
	tasksMu.Lock()
	defer tasksMu.Unlock()

	json.NewEncoder(w).Encode(tasks)
}

// Get a task by ID (GET /tasks/{id})
func getTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Convert ID from string to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	tasksMu.Lock()
	defer tasksMu.Unlock()

	// Search for the task by ID
	for _, task := range tasks {
		if task.ID == id {
			json.NewEncoder(w).Encode(task)
			return
		}
	}

	// If task not found
	http.Error(w, "Task not found", http.StatusNotFound)
}

// Update an existing task (PUT /tasks/{id})
func updateTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Convert ID from string to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	var updatedTask Task
	// Decode the request body into a Task struct
	err = json.NewDecoder(r.Body).Decode(&updatedTask)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tasksMu.Lock()
	defer tasksMu.Unlock()

	// Search for the task by ID and update it
	for i, task := range tasks {
		if task.ID == id {
			if updatedTask.Title != "" {
				tasks[i].Title = updatedTask.Title
			}
			if updatedTask.Description != "" {
				tasks[i].Description = updatedTask.Description
			}
			if updatedTask.Status != "" {
				tasks[i].Status = updatedTask.Status
			}

			json.NewEncoder(w).Encode(tasks[i])
			return
		}
	}

	// If task not found
	http.Error(w, "Task not found", http.StatusNotFound)
}

// Delete a task by ID (DELETE /tasks/{id})
func deleteTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Convert ID from string to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	tasksMu.Lock()
	defer tasksMu.Unlock()

	// Search for the task by ID and delete it
	for i, task := range tasks {
		if task.ID == id {
			tasks = append(tasks[:i], tasks[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// If task not found
	http.Error(w, "Task not found", http.StatusNotFound)
}

/////////////////////////////
// WebSocket Chat Handlers //
/////////////////////////////

// Handle WebSocket connections
func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer ws.Close()

	// Register new client
	clients[ws] = true

	for {
		var msg Message
		// Read new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			delete(clients, ws)
			break
		}
		// Send the newly received message to the broadcast channel
		broadcast <- msg
	}
}

// Broadcast messages to all connected clients
func handleMessages() {
	for {
		// Grab the next message from the broadcast channel
		msg := <-broadcast
		// Send it out to every client connected
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}
