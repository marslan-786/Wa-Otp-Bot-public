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

func handlePairAPILegacy(w http.ResponseWriter, r *http.Request) {
	// 🔥 CORS Headers (تاکہ براؤزر بلاک نہ کرے)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 1. URL سے نمبر نکالنا
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"Invalid URL format. Use /link/pair/92300xxxx"}`, 400)
		return
	}
	number := strings.TrimSpace(parts[3])

	// 2. نمبر کی صفائی
	number = strings.ReplaceAll(number, "+", "")
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")
	
	if len(number) < 10 {
		http.Error(w, `{"error":"Invalid phone number length"}`, 400)
		return
	}

	cleanNum := getCleanID(number)
	fmt.Printf("📱 [PAIRING-GET] Request for: %s\n", cleanNum)

	// 3. پرانے سیشنز کی صفائی (Active Clients + Database)
	// یہ حصہ بہت اہم ہے تاکہ "Connection Failed" نہ آئے۔
	
	// A. میموری سے صاف کریں
	clientsMutex.Lock()
	if c, ok := activeClients[cleanNum]; ok {
		fmt.Printf("🔌 [CLEANUP] Disconnecting active session for %s\n", cleanNum)
		c.Disconnect()
		delete(activeClients, cleanNum)
	}
	clientsMutex.Unlock()

	// B. ڈیٹا بیس سے صاف کریں
	devices, _ := container.GetAllDevices(context.Background())
	for _, dev := range devices {
		if getCleanID(dev.ID.User) == cleanNum {
			fmt.Printf("🧹 [DB] Deleting old session from DB for %s\n", cleanNum)
			dev.Delete(context.Background())
		}
	}

	// 4. نیا ڈیوائس اور کلائنٹ بنانا
	newDevice := container.NewDevice()
	tempClient := whatsmeow.NewClient(newDevice, waLog.Stdout("Pairing", "INFO", true))
	
	// ہینڈلرز شامل کریں
	tempClient.AddEventHandler(func(evt interface{}) {
		handler(tempClient, evt)
	})

	// 5. کنیکٹ کریں
	if err := tempClient.Connect(); err != nil {
		fmt.Printf("❌ [CONNECT FAIL] %v\n", err)
		http.Error(w, fmt.Sprintf(`{"error":"Connection failed: %v"}`, err), 500)
		return
	}

	// 6. پیئرنگ کوڈ جنریٹ کریں
	// تھوڑا سا انتظار تاکہ کنکشن مستحکم ہو جائے
	time.Sleep(2 * time.Second)

	code, err := tempClient.PairPhone(context.Background(), number, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		fmt.Printf("❌ [PAIR FAIL] %v\n", err)
		tempClient.Disconnect()
		http.Error(w, fmt.Sprintf(`{"error":"Pairing Error: %v"}`, err), 500)
		return
	}

	fmt.Printf("✅ [CODE GEN] %s -> %s\n", cleanNum, code)

	// 7. بیک گراؤنڈ میں لاگ ان کا انتظار کریں
	go func() {
		// 60 سیکنڈ تک چیک کریں کہ لاگ ان ہوا یا نہیں
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if tempClient.Store.ID != nil {
				fmt.Printf("🎉 [SUCCESS] %s Logged in successfully via GET API!\n", cleanNum)
				
				// ایکٹیو لسٹ میں ڈالیں
				clientsMutex.Lock()
				activeClients[cleanNum] = tempClient
				clientsMutex.Unlock()
				
				// ڈیٹا بیس میں پریفکس سیٹ کریں (Default)
				updatePrefixDB(cleanNum, ".")
				
				return
			}
		}
		// اگر لاگ ان نہیں ہوا تو بند کر دیں
		fmt.Printf("⌛ [TIMEOUT] Pairing timed out for %s\n", cleanNum)
		tempClient.Disconnect()
	}()

	// 8. HTML کو جواب بھیجیں
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"success": "true",
		"code":    code,
		"number":  cleanNum,
	})
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
