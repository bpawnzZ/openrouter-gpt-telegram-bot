package main

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/sashabaranov/go-openai"
	"log"
	"openrouter-gpt-telegram-bot/api"
	"openrouter-gpt-telegram-bot/config"
	"openrouter-gpt-telegram-bot/user"
	"strconv"
)

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	//llms.A1(conf)

	bot, err := tgbotapi.NewBotAPI(conf.TelegramBotToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false

	// Delete the webhook
	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		log.Fatalf("Failed to delete webhook: %v", err)
	}

	// Now you can safely use getUpdates
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	clientOptions := openai.DefaultConfig(conf.OpenAIApiKey)
	clientOptions.BaseURL = conf.OpenAIBaseURL
	client := openai.NewClientWithConfig(clientOptions)

	userManager := user.NewUserManager("logs")

	for update := range updates {
		if update.Message == nil {
			continue
		}
		userStats := userManager.GetUser(update.SentFrom().ID, update.SentFrom().UserName, conf)
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "help":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Available commands: /help, /reset, /stats, /stop")
				bot.Send(msg)
			case "reset":
				userStats.ClearHistory()
				args := update.Message.CommandArguments()
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "History cleared.")
				if args == "system" {
					userStats.SystemPrompt = conf.SystemPrompt
					msg.Text = "History cleared. System prompt set to default."
				} else if args != "" {
					msg.Text = "History cleared. System prompt set to " + args + "."
					userStats.SystemPrompt = args
				}
				bot.Send(msg)
			case "stats":
				userStats.CheckHistory(conf.MaxHistorySize, conf.MaxHistoryTime)
				usage := strconv.FormatFloat(userStats.GetCurrentCost(conf.BudgetPeriod), 'f', 6, 64)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Current usage: "+usage+"$ Messages amount: "+strconv.Itoa(len(userStats.GetMessages())))
				bot.Send(msg)
			case "stop":
				if userStats.CurrentStream != nil {
					userStats.CurrentStream.Close()
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "There is no active stream.")
					bot.Send(msg)
				}
			}
		} else {
			go func(userStats *user.UsageTracker) {
				// Handle user message
				if userStats.HaveAccess(conf) {
					responseID := api.HandleChatGPTStreamResponse(bot, client, update.Message, conf, userStats)
					userStats.GetUsageFromApi(responseID, conf)
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "You have exceeded your budget limit.")
					_, err := bot.Send(msg)
					if err != nil {
						log.Println(err)
					}
				}

			}(userStats)
		}
	}

}
