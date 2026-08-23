package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/justinpaulosolo/duebot/internal/gateway"

	"github.com/justinpaulosolo/duebot/internal/discord"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found or failed to load: %v", err)
	}

	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is not set")
	}

	rest := discord.NewREST(token)

	client := &gateway.Client{
		Token: token,
		Intents: gateway.IntentGuilds |
			gateway.IntentGuildMessages |
			gateway.IntentDirectMessages |
			gateway.IntentMessageContent,
	}

	client.OnMessageCreate = func(m gateway.MessageCreate) {
		log.Printf("message from %s: %s", m.Author.Username, m.Content)

		// This is where reminder-acknowledgment logic plugs in later:
		// look up whether m.ChannelID / m.Author.ID has a pending reminder,
		// and if so mark it acknowledged and stop re-sending it.
		if err := rest.SendMessage(m.ChannelID, "got it, thanks "+m.Author.Username); err != nil {
			log.Printf("failed to send reply: %v", err)
		}
	}

	go func() {
		if err := client.Run(); err != nil {
			log.Fatalf("gateway client stopped: %v", err)
		}
	}()

	// TODO: start the Canvas polling ticker + reminder scheduler here as
	// their own goroutine, using rest.CreateDMChannel + rest.SendMessage
	// to deliver reminders.

	// Block until Ctrl+C / SIGTERM (e.g. from systemd stop), then shut down
	// cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
	client.Close()
}
