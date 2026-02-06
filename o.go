package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

func StartOTPMonitor() {
	fmt.Println("👀 OTP Monitor Started... (Checking every 10s)")
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
		fmt.Printf("❌ API %d Error: %v\n", apiIdx, err)
		return
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		fmt.Printf("❌ API %d JSON Error: %v\n", apiIdx, err)
		return
	}

	if data["aaData"] == nil {
		// fmt.Printf("⚠️ API %d: No 'aaData'\n", apiIdx)
		return
	}
	aaData := data["aaData"].([]interface{})

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

		msgID := fmt.Sprintf("%v_%v", phone, rawTime)

		if IsOTPSent(msgID) {
			continue
		}

		// --- NEW OTP FOUND ---
		fmt.Printf("🔥 [NEW OTP] %s | API %d\n", phone, apiIdx)

		cleanCountry := cleanCountryName(countryRaw)
		cFlag, _ := GetCountryWithFlag(cleanCountry)
		otpCode := extractOTP(fullMsg)
		maskedPhone := maskPhoneNumber(phone)
		flatMsg := strings.ReplaceAll(strings.ReplaceAll(fullMsg, "\n", " "), "\r", "")

		ClientMutex.Lock()
		
		activeSessionCount := 0
		sentCount := 0

		for jidStr, cli := range ActiveClients {
			activeSessionCount++
			
			if cli.IsConnected() && cli.IsLoggedIn() {
				settings := GetUserSettings(jidStr)
				
				// 🔍 Debugging Logic
				fmt.Printf("   👤 Checking Session: %s | Channels: %d\n", jidStr, len(settings.Channels))

				if len(settings.Channels) > 0 {
					messageBody := formatMessage(cFlag, service, apiIdx, rawTime, cleanCountry, maskedPhone, otpCode, flatMsg, settings.CustomLink)
					
					for _, ch := range settings.Channels {
						jid, _ := types.ParseJID(ch)
						
						// 📤 Sending Message
						fmt.Printf("      📤 Sending to Channel: %s ... ", ch)
						
						_, err := cli.SendMessage(context.Background(), jid, &waProto.Message{
							Conversation: proto.String(strings.TrimSpace(messageBody)),
						})
						
						if err != nil {
							fmt.Printf("❌ FAILED: %v\n", err)
						} else {
							fmt.Printf("✅ SUCCESS!\n")
							sentCount++
						}
					}
				} else {
					fmt.Printf("      ⚠️ No Channels Set for this user. (Use .active command)\n")
				}
			} else {
				fmt.Printf("   🚫 Session %s Disconnected/Not Logged In\n", jidStr)
			}
		}
		ClientMutex.Unlock()

		if activeSessionCount == 0 {
			fmt.Println("⚠️ No Active Sessions found to broadcast.")
		}

		// Mark as sent regardless (taake loop mein na phanse)
		MarkOTPSent(msgID)
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
		"> © Developed by Nothing Is Impossible",
		cFlag, strings.ToUpper(service), apiIdx,
		rawTime, cFlag, country, phone, service, otp, link, fullMsg)
}
