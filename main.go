package main

import (
	"context" // Context add kia hai
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "github.com/mattn/go-sqlite3"
)

var (
	container     *sqlstore.Container
	ActiveClients = make(map[string]*whatsmeow.Client)
	ClientMutex   sync.Mutex
)

func main() {
	fmt.Println("🚀 Starting Kami Public Multi-Bot...")

	// 1. Initialize SQLite Database (Settings & Sessions)
	InitDB()
	
	// Initialize Whatsmeow Container with SQLite
	dbLog := waLog.Stdout("Database", "ERROR", true)
	var err error
	
	// FIX: Added context.Background() as the first argument
	container, err = sqlstore.New(context.Background(), "sqlite3", "file:./data/kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	// 2. Load Existing Sessions
	loadSessions()

	// 3. Start OTP Monitor (Background)
	go StartOTPMonitor()

	// 4. Start HTTP Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	http.HandleFunc("/", handleHome)
	http.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "pic.png")
	})
	http.HandleFunc("/link/pair/", handlePairAPI)
	http.HandleFunc("/link/delete", handleDeleteSession)

	go func() {
		fmt.Printf("🌐 Server listening on 0.0.0.0:%s\n", port)
		if err := http.ListenAndServe("0.0.0.0:"+port, nil); err != nil {
			panic(err)
		}
	}()

	// 5. Keep Alive
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	
	fmt.Println("\n🛑 Shutting down...")
	ClientMutex.Lock()
	for _, cli := range ActiveClients {
		cli.Disconnect()
	}
	ClientMutex.Unlock()
}

func loadSessions() {
	// FIX: Added context.Background()
	deviceStore, err := container.GetAllDevices(context.Background())
	if err != nil {
		fmt.Println("⚠️ Error getting devices:", err)
		return
	}

	for _, device := range deviceStore {
		client := whatsmeow.NewClient(device, waLog.Stdout("Client", "ERROR", true))
		client.AddEventHandler(EventHandler(client))
		
		if client.Store.ID != nil {
			err := client.Connect()
			if err != nil {
				fmt.Printf("❌ Failed to connect %s: %v\n", client.Store.ID, err)
			} else {
				ClientMutex.Lock()
				ActiveClients[client.Store.ID.ToNonAD().String()] = client
				ClientMutex.Unlock()
				fmt.Printf("✅ Loaded Session: %s\n", client.Store.ID.ToNonAD().String())
			}
		}
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// --- API Endpoints ---

func handlePairAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"Invalid URL. Use /link/pair/NUMBER"}`, 400)
		return
	}
	number := parts[3]

	fmt.Printf("📱 Pairing Request: %s\n", number)

	device := container.NewDevice()
	client := whatsmeow.NewClient(device, waLog.Stdout("Pairing", "INFO", true))
	
	client.AddEventHandler(EventHandler(client))

	if err := client.Connect(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	code, err := client.PairPhone(context.Background(), number, true, whatsmeow.PairClientChrome, "Linux")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	go func() {
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if client.Store.ID != nil {
				fmt.Printf("✅ Login Successful: %s\n", number)
				ClientMutex.Lock()
				ActiveClients[client.Store.ID.ToNonAD().String()] = client
				ClientMutex.Unlock()
				return
			}
		}
		client.Disconnect()
	}()

	json.NewEncoder(w).Encode(map[string]string{"code": code, "number": number})
}

func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	ClientMutex.Lock()
	defer ClientMutex.Unlock()
	
	for id, cli := range ActiveClients {
		cli.Disconnect()
		// FIX: Added context.Background()
		cli.Store.Delete(context.Background()) 
		delete(ActiveClients, id)
	}
	
	json.NewEncoder(w).Encode(map[string]string{"status": "All sessions deleted"})
}
