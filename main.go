package main

import (
	"fmt"
	"log"
	"openrouter-bot/api"
	"openrouter-bot/config"
	"openrouter-bot/lang"
	"openrouter-bot/user"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/sashabaranov/go-openai"
)
// getBotCommands — формирует список команд в меню в зависимости от роли
func getBotCommands(userID int64, conf *config.Config) []tgbotapi.BotCommand {
    userIDStr := strconv.FormatInt(userID, 10)
    isAdmin := false
    for _, adminID := range conf.AdminChatIDs {
        if userIDStr == fmt.Sprintf("%d", adminID) {
            isAdmin = true
            break
        }
    }

    if isAdmin {
        // Админ видит ВСЁ
        return []tgbotapi.BotCommand{
            {Command: "start", Description: lang.Translate("description.start", conf.Lang)},
            {Command: "help", Description: lang.Translate("description.help", conf.Lang)},
            {Command: "get_models", Description: lang.Translate("description.getModels", conf.Lang)},
            {Command: "set_model", Description: lang.Translate("description.setModel", conf.Lang)},
            {Command: "reset", Description: lang.Translate("description.reset", conf.Lang)},
            {Command: "stats", Description: lang.Translate("description.stats", conf.Lang)},
            {Command: "stop", Description: lang.Translate("description.stop", conf.Lang)},
            {Command: "пирдун", Description: "Задать вопрос модели"},
        }
    }

    // Все остальные — только start и пирдун
    return []tgbotapi.BotCommand{
        {Command: "start", Description: lang.Translate("description.start", conf.Lang)},
        {Command: "пирдун", Description: "Задать вопрос модели"},
    }
}
func main() {
	err := lang.LoadTranslations("./lang/")
	if err != nil {
		log.Fatalf("Error loading translations: %v", err)
	}

	manager, err := config.NewManager("./config.yaml")
	if err != nil {
		log.Fatalf("Error initializing config manager: %v", err)
	}

	conf := manager.GetConfig()

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

	// Set bot commands
	//commands := []tgbotapi.BotCommand{
	//	{Command: "start", Description: lang.Translate("description.start", conf.Lang)},
	//	{Command: "help", Description: lang.Translate("description.help", conf.Lang)},
	//	{Command: "get_models", Description: lang.Translate("description.getModels", conf.Lang)},
	//	{Command: "set_model", Description: lang.Translate("description.setModel", conf.Lang)},
	//	{Command: "reset", Description: lang.Translate("description.reset", conf.Lang)},
	//	{Command: "stats", Description: lang.Translate("description.stats", conf.Lang)},
	//	{Command: "stop", Description: lang.Translate("description.stop", conf.Lang)},
	//}
	//_, err = bot.Request(tgbotapi.NewSetMyCommands(commands...))
	//if err != nil {
	//	log.Fatalf("Failed to set bot commands: %v", err)
	//}

	clientOptions := openai.DefaultConfig(conf.OpenAIApiKey)
	clientOptions.BaseURL = conf.OpenAIBaseURL
	client := openai.NewClientWithConfig(clientOptions)

	userManager := user.NewUserManager("logs")

	for update := range updates {
		if update.Message == nil {
			continue
		}

		userStats := userManager.GetUser(update.SentFrom().ID, update.SentFrom().UserName, conf)

		// === ДИНАМИЧЕСКИ УСТАНАВЛИВАЕМ МЕНЮ КОМАНД ДЛЯ КАЖДОГО ПОЛЬЗОВАТЕЛЯ ===
		go func(userID int64) {
			commands := getBotCommands(userID, conf)
			bot.Request(tgbotapi.NewSetMyCommands(commands...))
		}(update.Message.From.ID)

		if update.Message.IsCommand() {
			cmd := update.Message.Command()

			// Проверяем, является ли пользователь админом
			isAdmin := false
			userIDStr := strconv.FormatInt(update.Message.From.ID, 10)
			for _, adminID := range conf.AdminChatIDs {
				if userIDStr == fmt.Sprintf("%d", adminID) {
					isAdmin = true
					break
				}
			}

			// Разрешённые команды для всех
			if cmd == "start" || cmd == "пирдун" {
				// Эти команды работают у всех
			} else {
				// Все остальные команды — ТОЛЬКО для админов
				if !isAdmin {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Эта команда доступна только администраторам бота.")
					msg.ParseMode = "HTML"
					bot.Send(msg)
					continue
				}
			}

			switch cmd {

			case "start":
				msgText := lang.Translate("commands.start", conf.Lang) + lang.Translate("commands.help", conf.Lang) + lang.Translate("commands.start_end", conf.Lang)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
				msg.ParseMode = "HTML"
				bot.Send(msg)

			case "пирдун":
				args := update.Message.CommandArguments()
				if args == "" {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Использование: /пирдун <текст запроса>")
					bot.Send(msg)
					continue
				}
				fakeMsg := *update.Message
				fakeMsg.Text = args
				go func(userStats *user.UsageTracker) {
					if userStats.HaveAccess(conf) {
						responseID := api.HandleChatGPTStreamResponse(bot, client, &fakeMsg, conf, userStats)
						if conf.Model.Type == "openrouter" {
							userStats.GetUsageFromApi(responseID, conf)
						}
					} else {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("budget_out", conf.Lang))
						bot.Send(msg)
					}
				}(userStats)

			// === ВСЕ ОСТАЛЬНЫЕ КОМАНДЫ — ТОЛЬКО ДЛЯ АДМИНОВ ===
			case "help":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.help", conf.Lang))
				msg.ParseMode = "HTML"
				bot.Send(msg)

			case "get_models":
				models, _ := api.GetFreeModels()
				text := lang.Translate("commands.getModels", conf.Lang) + models
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
				msg.ParseMode = tgbotapi.ModeMarkdown
				bot.Send(msg)

			case "set_model":
				args := update.Message.CommandArguments()
				argsArr := strings.Split(args, " ")
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, conf.Model.ModelName)
				msg.ParseMode = tgbotapi.ModeMarkdown
				switch {
				case args == "default":
					conf.Model.ModelName = conf.Model.ModelNameDefault
					msg.Text = lang.Translate("commands.setModel", conf.Lang) + " `" + conf.Model.ModelName + "`"
				case args == "":
					msg.Text = lang.Translate("commands.noArgsModel", conf.Lang)
				case len(argsArr) > 1:
					msg.Text = lang.Translate("commands.noSpaceModel", conf.Lang)
				default:
					conf.Model.ModelName = argsArr[0]
					msg.Text = lang.Translate("commands.setModel", conf.Lang) + " `" + conf.Model.ModelName + "`"
				}
				bot.Send(msg)

			case "reset":
				args := update.Message.CommandArguments()
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")
				if args == "system" {
					userStats.SystemPrompt = conf.SystemPrompt
					msg.Text = lang.Translate("commands.reset_system", conf.Lang)
				} else if args != "" {
					userStats.SystemPrompt = args
					msg.Text = lang.Translate("commands.reset_prompt", conf.Lang) + args + "."
				} else {
					userStats.ClearHistory()
					msg.Text = lang.Translate("commands.reset", conf.Lang)
				}
				bot.Send(msg)

			case "stats":
				userStats.CheckHistory(conf.MaxHistorySize, conf.MaxHistoryTime)
				countedUsage := strconv.FormatFloat(userStats.GetCurrentCost(conf.BudgetPeriod), 'f', 6, 64)
				todayUsage := strconv.FormatFloat(userStats.GetCurrentCost("daily"), 'f', 6, 64)
				monthUsage := strconv.FormatFloat(userStats.GetCurrentCost("monthly"), 'f', 6, 64)
				totalUsage := strconv.FormatFloat(userStats.GetCurrentCost("total"), 'f', 6, 64)
				messagesCount := strconv.Itoa(len(userStats.GetMessages()))
				statsMessage := fmt.Sprintf(lang.Translate("commands.stats", conf.Lang), countedUsage, todayUsage, monthUsage, totalUsage, messagesCount)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, statsMessage)
				msg.ParseMode = "HTML"
				bot.Send(msg)

			case "stop":
				if userStats.CurrentStream != nil {
					userStats.CurrentStream.Close()
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.stop", conf.Lang))
					bot.Send(msg)
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.stop_err", conf.Lang))
					bot.Send(msg)
				}
			}
		} else {
			// Обычные сообщения (не команды) — работают у всех, кто имеет доступ по бюджету
			go func(userStats *user.UsageTracker) {
				if userStats.HaveAccess(conf) {
					responseID := api.HandleChatGPTStreamResponse(bot, client, update.Message, conf, userStats)
					if conf.Model.Type == "openrouter" {
						userStats.GetUsageFromApi(responseID, conf)
					}
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("budget_out", conf.Lang))
					bot.Send(msg)
				}
			}(userStats)
		}
	}

}
