package main

const DefaultLink = "https://chat.whatsapp.com/YourDefaultLinkHere"

var Config = struct {
	OTPApiURLs []string
	Interval   int
}{
	OTPApiURLs: []string{
		"https://api-kami-nodejs-production.up.railway.app/api?type=sms",
		"https://kamina-otp.up.railway.app/d-group/sms",
		"https://kamina-otp.up.railway.app/npm-neon/sms",
		"https://kamina-otp.up.railway.app/mait/sms",
	},
	Interval: 5,
}
