package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	db       *sql.DB
	dbMutex  sync.Mutex
	dbPath   = "./data/kami_bot.db"
)

type UserSettings struct {
	JID        string
	Channels   []string
	CustomLink string
}

func InitDB() {
	// Ensure data directory exists
	if _, err := os.Stat("./data"); os.IsNotExist(err) {
		os.Mkdir("./data", 0755)
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		panic("❌ Could not connect to SQLite: " + err.Error())
	}

	// Table for User Settings (Channels & Links)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS user_settings (
		jid TEXT PRIMARY KEY,
		channels TEXT,
		custom_link TEXT
	)`)
	if err != nil {
		panic(err)
	}

	// Table for Sent OTP History (Global Deduplication)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sent_history (
		msg_id TEXT PRIMARY KEY,
		created_at DATETIME
	)`)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ SQLite Database Initialized at", dbPath)
}

// --- User Settings Functions ---

func GetUserSettings(jid string) UserSettings {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	var channelsJSON, link string
	row := db.QueryRow("SELECT channels, custom_link FROM user_settings WHERE jid = ?", jid)
	err := row.Scan(&channelsJSON, &link)

	settings := UserSettings{JID: jid, CustomLink: DefaultLink}
	
	if err == nil {
		json.Unmarshal([]byte(channelsJSON), &settings.Channels)
		if link != "" {
			settings.CustomLink = link
		}
	} else {
		// Return empty settings if not found
		settings.Channels = []string{}
	}
	return settings
}

func AddChannel(jid, channelID string) error {
	settings := GetUserSettings(jid)
	// Check if already exists
	for _, ch := range settings.Channels {
		if ch == channelID {
			return fmt.Errorf("Channel already added")
		}
	}
	settings.Channels = append(settings.Channels, channelID)
	return saveSettings(settings)
}

func RemoveChannel(jid, channelID string) error {
	settings := GetUserSettings(jid)
	newChannels := []string{}
	found := false
	for _, ch := range settings.Channels {
		if ch == channelID {
			found = true
			continue
		}
		newChannels = append(newChannels, ch)
	}
	if !found {
		return fmt.Errorf("Channel not found")
	}
	settings.Channels = newChannels
	return saveSettings(settings)
}

func SetCustomLink(jid, link string) error {
	settings := GetUserSettings(jid)
	settings.CustomLink = link
	return saveSettings(settings)
}

func saveSettings(s UserSettings) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()
	
	data, _ := json.Marshal(s.Channels)
	_, err := db.Exec(`INSERT INTO user_settings (jid, channels, custom_link) VALUES (?, ?, ?) 
		ON CONFLICT(jid) DO UPDATE SET channels = ?, custom_link = ?`,
		s.JID, string(data), s.CustomLink, string(data), s.CustomLink)
	return err
}


func IsOTPSent(id string) bool {
	dbMutex.Lock()
	defer dbMutex.Unlock()
	var exists int
	err := db.QueryRow("SELECT 1 FROM sent_history WHERE msg_id = ?", id).Scan(&exists)
	return err == nil
}

func MarkOTPSent(id string) {
	dbMutex.Lock()
	defer dbMutex.Unlock()
	db.Exec("INSERT INTO sent_history (msg_id, created_at) VALUES (?, ?)", id, time.Now())
	
	// Optional: Cleanup old history every now and then (not implemented here for brevity)
}
