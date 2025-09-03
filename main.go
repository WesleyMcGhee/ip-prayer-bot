package main

import (
	"log"
  crand "crypto/rand"
  "encoding/hex"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

const ephemeralFlag = discordgo.MessageFlagsEphemeral

func strPtr(s string) *string { return &s }

var (
	modChannelID    string
	publicChannelID string
)

var prayerRequests = make(map[string]struct {
  UserID   string
  Username string
  Text     string
})

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ No .env file found, relying on real environment variables")
	}
}

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	modChannelID = os.Getenv("MOD_CHANNEL_ID")
	publicChannelID = os.Getenv("PUBLIC_CHANNEL_ID")

	if token == "" || modChannelID == "" || publicChannelID == "" {
		log.Fatal("Missing required env vars: DISCORD_TOKEN, MOD_CHANNEL_ID, PUBLIC_CHANNEL_ID")
	}

	dg, err := discordgo.New(token)
	if err != nil {
		log.Fatal("Error creating Discord session:", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds

	dg.AddHandler(onInteraction)

	if err := dg.Open(); err != nil {
		log.Fatal("Error opening Discord session:", err)
	}
	log.Println("✅ Connected to Discord")

	_, err = dg.ApplicationCommandCreate(dg.State.User.ID, "", &discordgo.ApplicationCommand{
		Name:        "prayer",
		Description: "Submit a prayer request (goes to moderators first)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "request",
				Description: "Your prayer request",
				Required:    true,
			},
		},
	})
	if err != nil {
		log.Fatal("Cannot create command:", err)
	}

	select {}
}

func onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		if i.ApplicationCommandData().Name == "prayer" {
			handlePrayer(s, i)
		}
	case discordgo.InteractionMessageComponent:
		handleButton(s, i)
	}
}

func handlePrayer(s *discordgo.Session, i *discordgo.InteractionCreate) {
	prayerText := i.ApplicationCommandData().Options[0].StringValue()
	userID := i.Member.User.ID
  userName := i.Member.User.Username

  id := newID()
  prayerRequests[id] = struct {
    UserID string
    Username string
    Text string
  }{
    UserID: userID,
    Username: userName,
    Text: prayerText,
  }

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   ephemeralFlag,
			Content: "🙏 Your prayer request has been sent to the moderators for review.",
		},
	}); err != nil {
		log.Println("respond(/prayer) failed:", err)
		return
	}

  _, err := s.ChannelMessageSendComplex(modChannelID, &discordgo.MessageSend{
    Content: "New prayer request from **" + userName + "**:",
    Embeds: []*discordgo.MessageEmbed{
        {
            Description: prayerText,
            Color:       0x5865F2,
        },
    },
    Components: []discordgo.MessageComponent{
        discordgo.ActionsRow{
            Components: []discordgo.MessageComponent{
                discordgo.Button{
                    Label:    "Accept",
                    Style:    discordgo.SuccessButton,
                    CustomID: "accept:" + id,
                },
                discordgo.Button{
                    Label:    "Reject",
                    Style:    discordgo.DangerButton,
                    CustomID: "reject:" + id,
                },
            },
        },
    },
})
if err != nil {
    log.Println("failed to send mod message:", err)
}

}

func handleButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	parts := strings.SplitN(i.MessageComponentData().CustomID, ":", 2)
	if len(parts) != 2 {
		return
	}
	action, id := parts[0], parts[1]
  req, ok := prayerRequests[id]
  if !ok {
    return
  }

	// Always ack quickly to avoid timeouts
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		log.Println("button defer failed:", err)
		return
	}

	switch action {
	case "accept":
		// Post anonymously to public channel
		if _, err := s.ChannelMessageSend(publicChannelID, "🙏 **Prayer Request:**\n"+req.Text); err != nil {
			log.Println("failed to post public prayer:", err)
		}
		// Edit moderator message
		_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    strPtr("✅ Prayer request approved and posted."),
			Embeds:     &[]*discordgo.MessageEmbed{},
			Components: &[]discordgo.MessageComponent{},
		})
		if err != nil {
			log.Println("edit after accept failed:", err)
		}

	case "reject":
		_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    strPtr("❌ Prayer request rejected."),
			Embeds:     &[]*discordgo.MessageEmbed{},
			Components: &[]discordgo.MessageComponent{},
		})
		if err != nil {
			log.Println("edit after reject failed:", err)
		}
		// Try to DM user privately
		if req.UserID != "" {
			if ch, err := s.UserChannelCreate(req.UserID); err == nil {
				_, _ = s.ChannelMessageSend(ch.ID, "⚠️ Your prayer request was not approved by the moderators.")
			}
		}
	}
  delete(prayerRequests, id)
}

func newID() string {
  b := make([]byte, 8)
  if _, err := crand.Read(b); err != nil {
    panic(err)
  }
  return hex.EncodeToString(b)
}
