package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

var (
	LidMap   = make(map[string]string)
	LidMutex sync.RWMutex
)

func InitLIDSystem() {
	db, err := sql.Open("sqlite3", "file:./data/kami_sessions.db?mode=ro") // Read Only Mode
	if err != nil {
		fmt.Println("⚠️ [LID] Could not open sessions DB:", err)
		return
	}
	defer db.Close()

	// ٹیبل سے ڈیٹا نکالیں
	rows, err := db.Query("SELECT jid, lid FROM whatsmeow_device")
	if err != nil {
		fmt.Println("⚠️ [LID] Table query failed (Maybe no sessions yet):", err)
		return
	}
	defer rows.Close()

	count := 0
	LidMutex.Lock()
	for rows.Next() {
		var rawJID, rawLID sql.NullString
		if err := rows.Scan(&rawJID, &rawLID); err != nil {
			continue
		}

		if rawJID.Valid && rawLID.Valid && rawLID.String != "" {
	
			phone := getCleanID(rawJID.String)
			lid := getCleanID(rawLID.String)

			LidMap[lid] = phone
			count++
		
		}
	}
	LidMutex.Unlock()

	fmt.Printf("💎 [LID SYSTEM] Loaded %d linked identities into memory.\n", count)
}

func ResolveJID(inputJID string) string {
	cleanInput := getCleanID(inputJID)

	LidMutex.RLock()
	realPhone, exists := LidMap[cleanInput]
	LidMutex.RUnlock()

	if exists {
		// fmt.Printf("🔄 [RESOLVE] Swapped LID %s with Phone %s\n", cleanInput, realPhone)
		return realPhone
	}

	return cleanInput
}

func RefreshLIDCache() {
	go InitLIDSystem()
}
