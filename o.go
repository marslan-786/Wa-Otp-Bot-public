package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

func StartOTPMonitor() {
	fmt.Println("👀 OTP Monitor Started...")
	for {
		for i, url := range Config.OTPApiURLs {
			apiIdx := i + 1
			processAPI(url, apiIdx)
		}
		time.Sleep(time.Duration(Config.Interval) * time.Second)
	}
}

func processAPI(url string, apiIdx int) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	if data["aaData"] == nil {
		return
	}
	aaData := data["aaData"].([]interface{})

	// Loop through API data
	for _, row := range aaData {
		r, ok := row.([]interface{})
		if !ok || len(r) < 5 {
			continue
		}

		rawTime := fmt.Sprintf("%v", r[0])
		countryRaw := fmt.Sprintf("%v", r[1])
		phone := fmt.Sprintf("%v", r[2])
		service := fmt.Sprintf("%v", r[3])
		fullMsg := fmt.Sprintf("%v", r[4])

		if phone == "0" || phone == "" {
			continue
		}

		// Unique ID for Global Deduplication
		msgID := fmt.Sprintf("%v_%v", phone, rawTime)

		if !IsOTPSent(msgID) {
			// 1. Prepare Data
			cleanCountry := cleanCountryName(countryRaw)
			cFlag, _ := GetCountryWithFlag(cleanCountry)
			otpCode := extractOTP(fullMsg)
			maskedPhone := maskPhoneNumber(phone)
			flatMsg := strings.ReplaceAll(strings.ReplaceAll(fullMsg, "\n", " "), "\r", "")

			// 2. Broadcast to ALL Connected Clients
			ClientMutex.Lock()
			for jidStr, cli := range ActiveClients {
				if cli.IsConnected() && cli.IsLoggedIn() {
					// Get User Specific Settings
					settings := GetUserSettings(jidStr)
					
					// Only send if user has active channels
					if len(settings.Channels) > 0 {
						// Custom Message Body for this user (Custom Link)
						messageBody := formatMessage(cFlag, service, apiIdx, rawTime, cleanCountry, maskedPhone, otpCode, flatMsg, settings.CustomLink)
						
						// Send to all user's channels
						for _, ch := range settings.Channels {
							jid, _ := types.ParseJID(ch)
							go cli.SendMessage(context.Background(), jid, &waProto.Message{
								Conversation: proto.String(strings.TrimSpace(messageBody)),
							})
						}
					}
				}
			}
			ClientMutex.Unlock()

			// 3. Mark as Sent Globally
			MarkOTPSent(msgID)
			fmt.Printf("✅ [Broadcast] API %d: %s\n", apiIdx, phone)
		}
	}
}

func formatMessage(cFlag, service string, apiIdx int, rawTime, country, phone, otp, fullMsg, link string) string {
	return fmt.Sprintf("✨ *%s | %s Message %d* ⚡\n\n"+
		"> *Time:* %s\n"+
		"> *Country:* %s %s\n"+
		"   *Number:* *%s*\n"+
		"> *Service:* %s\n"+
		"   *OTP:* *%s*\n\n"+
		"> *Join For Numbers:* \n"+
		"> %s\n\n"+
		"*Full Message:*\n"+
		"%s\n\n"+
		"> © Developed by 𝙎𝙞𝙡𝙚𝙣𝙩 𝙃𝙖𝙘𝙠𝙚𝙧𝙨",
		cFlag, strings.ToUpper(service), apiIdx,
		rawTime, cFlag, country, phone, service, otp, link, fullMsg)
}
