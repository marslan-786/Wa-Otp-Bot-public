package main

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

func EventHandler(cli *whatsmeow.Client) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			if !v.Info.IsFromMe {
				// Self-ignore removed so users can control their own bot via their own number if needed, 
				// but usually commands come from "Me" (Self) or other admins.
				// For a public bot, usually the owner commands it.
				// Allowing FromMe ensures user can set up their own bot.
			}
			handleCommands(cli, v)
		}
	}
}

func handleCommands(cli *whatsmeow.Client, evt *events.Message) {
	msgText := ""
	if evt.Message.GetConversation() != "" {
		msgText = evt.Message.GetConversation()
	} else if evt.Message.ExtendedTextMessage != nil {
		msgText = evt.Message.ExtendedTextMessage.GetText()
	}

	args := strings.Fields(msgText)
	if len(args) == 0 {
		return
	}

	cmd := strings.ToLower(args[0])
	userJID := cli.Store.ID.ToNonAD().String() // The bot's own number

	// Check if message is from the owner (Bot itself) or an allowed controller
	// For this Public Bot model, we assume the user controls their bot by messaging IT or sending messages to "Note to self"
	// Or messaging in a group.
	
	// IMPORTANT: Use the Sender JID to verify permission if needed.
	// For now, allowing commands if they match syntax.

	switch cmd {
	case ".id":
		sender := evt.Info.Sender.ToNonAD().String()
		chat := evt.Info.Chat.ToNonAD().String()
		reply(cli, evt, fmt.Sprintf("👤 *User:* `%s`\n📍 *Chat:* `%s`", sender, chat))

	case ".active":
		if len(args) < 2 {
			reply(cli, evt, "❌ Usage: .active <Channel_ID>")
			return
		}
		channelID := args[1]
		err := AddChannel(userJID, channelID)
		if err != nil {
			reply(cli, evt, "⚠️ Error: "+err.Error())
		} else {
			reply(cli, evt, "✅ Channel Activated!\nMessages will now flow to: "+channelID)
		}

	case ".deactive":
		if len(args) < 2 {
			reply(cli, evt, "❌ Usage: .deactive <Channel_ID>")
			return
		}
		channelID := args[1]
		err := RemoveChannel(userJID, channelID)
		if err != nil {
			reply(cli, evt, "⚠️ Error: "+err.Error())
		} else {
			reply(cli, evt, "✅ Channel Deactivated!")
		}

	case ".change":
		if len(args) < 2 {
			reply(cli, evt, "❌ Usage: .change <New_Link>")
			return
		}
		newLink := args[1]
		SetCustomLink(userJID, newLink)
		reply(cli, evt, "✅ Footer Link Updated!\nNew Link: "+newLink)

	case ".list":
		settings := GetUserSettings(userJID)
		msg := "📋 *Active Channels:*\n"
		if len(settings.Channels) == 0 {
			msg += "No active channels."
		} else {
			for _, ch := range settings.Channels {
				msg += fmt.Sprintf("- `%s`\n", ch)
			}
		}
		msg += "\n🔗 *Current Link:*\n" + settings.CustomLink
		reply(cli, evt, msg)
	}
}

func reply(cli *whatsmeow.Client, evt *events.Message, text string) {
	cli.SendMessage(context.Background(), evt.Info.Chat, &waProto.Message{
		Conversation: proto.String(text),
	})
}
