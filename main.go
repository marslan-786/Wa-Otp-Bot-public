package main

import (
	"context"
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
	"go.mau.fi/whatsmeow/store"
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
	fmt.Println("🚀 Starting Kami OTP Bot (Multi-Session)...")

	// 1. Initialize Database Tables (From database.go)
	InitDB()

	// 2. Initialize Whatsmeow Container (SQLite for Volume)
	dbLog := waLog.Stdout("Database", "ERROR", true)
	var err error
	// Railway Volume Path: ./data/
	os.MkdirAll("./data", 0755) 
	container, err = sqlstore.New("sqlite3", "file:./data/kami_sessions.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic("❌ Failed to initialize SQLite: " + err.Error())
	}

	// 3. Load Existing Sessions
	StartAllBots()

	// 4. Start OTP Monitor (From otp.go)
	go StartOTPMonitor()

	// 5. Setup HTTP Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Routes
	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/pic.png", servePicture)
	
	// Pairing Routes (Supports both POST & GET legacy)
	http.HandleFunc("/api/pair", handlePairAPIPost)     // POST JSON
	http.HandleFunc("/link/pair/", handlePairAPILegacy) // GET /link/pair/92300...
	http.HandleFunc("/link/delete", handleDeleteSession)

	// Start Server
	go func() {
		fmt.Printf("🌐 Server listening on :%s\n", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			panic(err)
		}
	}()

	// 6. Keep Alive / Shutdown Handling
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

// ---------------------------------------------------------
// 🔄 MULTI-SESSION CORE LOGIC
// ---------------------------------------------------------

func StartAllBots() {
	devices, err := container.GetAllDevices(context.Background())
	if err != nil {
		fmt.Printf("❌ Could not load sessions: %v\n", err)
		return
	}
	fmt.Printf("🤖 Found %d sessions in database. Loading...\n", len(devices))

	for _, device := range devices {
		go ConnectNewSession(device)
		time.Sleep(1 * time.Second) // Thora gap taake load na pare
	}
}

func ConnectNewSession(device *store.Device) {
	rawID := device.ID.User
	cleanID := getCleanID(rawID)

	ClientMutex.Lock()
	if _, exists := ActiveClients[cleanID]; exists {
		ClientMutex.Unlock()
		return // Already active
	}
	ClientMutex.Unlock()

	// Create Client
	clientLog := waLog.Stdout("Client", "ERROR", true)
	newBot := whatsmeow.NewClient(device, clientLog)

	// Hook the Handler (From handler.go)
	newBot.AddEventHandler(EventHandler(newBot))

	if err := newBot.Connect(); err != nil {
		fmt.Printf("❌ Failed to connect %s: %v\n", cleanID, err)
		return
	}

	ClientMutex.Lock()
	ActiveClients[cleanID] = newBot
	ClientMutex.Unlock()
	fmt.Printf("✅ [LOADED] Session: %s\n", cleanID)
}

// ---------------------------------------------------------
// 🌐 PAIRING LOGIC (The Logic You Wanted)
// ---------------------------------------------------------

// GET Request: /link/pair/923001234567
func handlePairAPILegacy(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"Invalid URL"}`, 400)
		return
	}
	rawNumber := parts[3]
	performPairing(w, rawNumber)
}

// POST Request: JSON Body {"number": "..."}
func handlePairAPIPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		Number string `json:"number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}
	performPairing(w, req.Number)
}

// Common Logic for Pairing
func performPairing(w http.ResponseWriter, rawNumber string) {
	number := strings.ReplaceAll(rawNumber, "+", "")
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")
	cleanNum := getCleanID(number)

	fmt.Printf("📱 [PAIRING] Request for: %s\n", cleanNum)

	// 1. Cleanup Old Session (Memory)
	ClientMutex.Lock()
	if c, ok := ActiveClients[cleanNum]; ok {
		c.Disconnect()
		delete(ActiveClients, cleanNum)
	}
	ClientMutex.Unlock()

	// 2. Cleanup Old Session (Database)
	devices, _ := container.GetAllDevices(context.Background())
	for _, dev := range devices {
		if getCleanID(dev.ID.User) == cleanNum {
			dev.Delete(context.Background())
		}
	}

	// 3. Create New Device
	device := container.NewDevice()
	client := whatsmeow.NewClient(device, waLog.Stdout("Pairing", "INFO", true))
	
	// Handler Add karein (taake login ke foran baad active ho)
	client.AddEventHandler(EventHandler(client))

	if err := client.Connect(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Connect failed: %v"}`, err), 500)
		return
	}

	// 4. Generate Code
	code, err := client.PairPhone(context.Background(), number, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		client.Disconnect()
		http.Error(w, fmt.Sprintf(`{"error":"Pairing failed: %v"}`, err), 500)
		return
	}

	// 5. Wait for Login (Background)
	go func() {
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if client.Store.ID != nil {
				fmt.Printf("🎉 [SUCCESS] %s Paired Successfully!\n", cleanNum)
				ClientMutex.Lock()
				ActiveClients[cleanNum] = client
				ClientMutex.Unlock()
				return
			}
		}
		// Timeout
		client.Disconnect()
	}()

	// 6. Respond to Web
	json.NewEncoder(w).Encode(map[string]string{
		"success": "true",
		"code":    code,
		"number":  cleanNum,
	})
}

// ---------------------------------------------------------
// 🗑️ CLEANUP API
// ---------------------------------------------------------

func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	ClientMutex.Lock()
	defer ClientMutex.Unlock()

	// Disconnect All
	for _, c := range ActiveClients {
		c.Disconnect()
	}
	ActiveClients = make(map[string]*whatsmeow.Client)

	// Delete DB
	devs, _ := container.GetAllDevices(context.Background())
	for _, d := range devs {
		d.Delete(context.Background())
	}
	
	json.NewEncoder(w).Encode(map[string]string{"status": "All Sessions Deleted"})
}

// ---------------------------------------------------------
// 🌐 STATIC FILES
// ---------------------------------------------------------

func serveHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func servePicture(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "pic.png")
}

// ---------------------------------------------------------
// 🛠️ UTILS
// ---------------------------------------------------------

func getCleanID(id string) string {
	if strings.Contains(id, ":") {
		id = strings.Split(id, ":")[0]
	}
	if strings.Contains(id, "@") {
		id = strings.Split(id, "@")[0]
	}
	return id
}
