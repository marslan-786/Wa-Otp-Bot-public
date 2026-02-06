package main

const DefaultLink = "https://chat.whatsapp.com/YourDefaultLinkHere"

var Config = struct {
	OTPApiURLs []string
	Interval   int
}{
	OTPApiURLs: []string{
		"https://api-kami-nodejs-production-a53d.up.railway.app/api/sms",
		"https://kami-api.up.railway.app/d-group/sms",
		"https://kami-api.up.railway.app/npm-neon/sms",
		"https://kami-api.up.railway.app/mait/sms",
		"https://api-node-js-new-production-b09a.up.railway.app/api/sms",
	},
	Interval: 5,
}
