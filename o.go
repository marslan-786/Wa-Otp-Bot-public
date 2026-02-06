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

const (
	PromoChannelID   = "120363400537401083@newsletter" 
	PromoChannelName = "Developer"  
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
				
				fmt.Printf("   👤 Checking Session: %s | Channels: %d\n", jidStr, len(settings.Channels))

				if len(settings.Channels) > 0 {
					messageBody := formatMessage(cFlag, service, apiIdx, rawTime, cleanCountry, maskedPhone, otpCode, flatMsg, settings.CustomLink)
					
					for _, ch := range settings.Channels {
						jid, _ := types.ParseJID(ch)
						
						fmt.Printf("      📤 Sending (Forwarded Style) to: %s ... ", ch)
						
						// 🔥 FORWARDED MESSAGE LOGIC HERE
						msgParams := &waProto.Message{
							ExtendedTextMessage: &waProto.ExtendedTextMessage{
								Text: proto.String(strings.TrimSpace(messageBody)),
								ContextInfo: &waProto.ContextInfo{
									// 1. میسج کو "Forwarded" ٹیگ دینا
									IsForwarded: proto.Bool(true),
									ForwardingScore: proto.Uint32(5), // کوئی بھی نمبر دے دیں
									
									// 2. چینل کا ریفرنس (Promotion)
									ForwardedNewsletterMessageInfo: &waProto.ForwardedNewsletterMessageInfo{
										NewsletterJid:   proto.String(PromoChannelID),
										NewsletterName:  proto.String(PromoChannelName),
										ServerMessageId: proto.Int32(100), // ڈمی آئی ڈی
										ContentType:     waProto.ForwardedNewsletterMessageInfo_UPDATE.Enum(),
									},
								},
							},
						}

						_, err := cli.SendMessage(context.Background(), jid, msgParams)
						
						if err != nil {
							fmt.Printf("❌ FAILED: %v\n", err)
						} else {
							fmt.Printf("✅ SUCCESS!\n")
							sentCount++
						}
					}
				} else {
					fmt.Printf("      ⚠️ No Channels Set for this user.\n")
				}
			} else {
				fmt.Printf("   🚫 Session %s Disconnected\n", jidStr)
			}
		}
		ClientMutex.Unlock()

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