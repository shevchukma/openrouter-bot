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

// === Определяем роль пользователя ===
func getUserRole(userID int64, conf *config.Config) string {
	userIDStr := strconv.FormatInt(userID, 10)

	for _, adminID := range conf.AdminChatIDs {
		if userIDStr == fmt.Sprintf("%d", adminID) {
			return "admin"
		}
	}
	for _, allowedID := range conf.AllowedUserChatIDs {
		if userIDStr == fmt.Sprintf("%d", allowedID) {
			return "user"
		}
	}
	return "guest"
}

// === Динамическое меню команд ===
func getBotCommands(userID int64, conf *config.Config) []tgbotapi.BotCommand {
	role := getUserRole(userID, conf)

	if role == "admin" {
		return []tgbotapi.BotCommand{
			{Command: "start", Description: lang.Translate("description.start", conf.Lang)},
			{Command: "help", Description: lang.Translate("description.help", conf.Lang)},
			{Command: "get_models", Description: lang.Translate("description.getModels", conf.Lang)},
			{Command: "set_model", Description: lang.Translate("description.setModel", conf.Lang)},
			{Command: "reset", Description: lang.Translate("description.reset", conf.Lang)},
			{Command: "stats", Description: lang.Translate("description.stats", conf.Lang)},
			{Command: "stop", Description: lang.Translate("description.stop", conf.Lang)},
			{Command: "pirdun", Description: lang.Translate("description.pirdun", conf.Lang)},
		}
	}

	if role == "user" {
		return []tgbotapi.BotCommand{
			{Command: "start", Description: lang.Translate("description.start", conf.Lang)},
			{Command: "help", Description: lang.Translate("description.helpuser", conf.Lang)},
			{Command: "reset", Description: lang.Translate("description.reset", conf.Lang)},
			{Command: "stop", Description: lang.Translate("description.stop", conf.Lang)},
			{Command: "pirdun", Description: lang.Translate("description.pirdun", conf.Lang)},
		}
	}

	// guest
	return []tgbotapi.BotCommand{
		{Command: "start", Description: lang.Translate("description.start", conf.Lang)},
		{Command: "help", Description: lang.Translate("description.helpuser", conf.Lang)},
		{Command: "pirdun", Description: lang.Translate("description.pirdun", conf.Lang)},
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

	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		log.Fatalf("Failed to delete webhook: %v", err)
	}

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
	
		// Пропускаем все служебные сообщения Telegram
		if update.Message.NewChatMembers != nil ||	  // кто-то добавлен (включая бота)
		   update.Message.LeftChatMember != nil ||	  // кто-то вышел
		   update.Message.GroupChatCreated ||		   // создан групповой чат
		   update.Message.SupergroupChatCreated ||	  // создан супергрупповой
		   update.Message.ChannelChatCreated ||		 // создан канал
		   update.Message.MigrateToChatID != 0 ||	   // миграция
		   update.Message.MigrateFromChatID != 0 {
			continue  // просто игнорируем
		}
	
		userID := update.Message.From.ID
		userStats := userManager.GetUser(userID, update.SentFrom().UserName, conf)
		role := getUserRole(userID, conf)

		// Обновляем меню команд для пользователя
		go bot.Request(tgbotapi.NewSetMyCommands(getBotCommands(userID, conf)...))

		// Гости могут только /start, /pirdun и обычные сообщения
		if role == "guest" && update.Message.IsCommand() {
			cmd := update.Message.Command()
			if cmd != "start" && cmd != "pirdun" {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет доступа к командам.")
				bot.Send(msg)
				continue
			}
		}

		if update.Message.IsCommand() {
			cmd := update.Message.Command()

			// Только админы могут использовать эти команды
			if cmd == "get_models" || cmd == "set_model" || cmd == "stats" {
				if role != "admin" {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Эта команда доступна только администраторам.")
					bot.Send(msg)
					continue
				}
			}

			switch cmd {
			case "start":
				text := lang.Translate("commands.start", conf.Lang)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
				msg.ParseMode = "HTML"
				bot.Send(msg)

			case "help":
				helpText := lang.Translate("commands.helpuser", conf.Lang)
			
				if role == "admin" {
					helpText = lang.Translate("commands.help", conf.Lang)
				}
			
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpText)
				msg.ParseMode = "HTML"
				bot.Send(msg)

			case "reset":
				userStats.ClearHistory()
				userStats.SystemPrompt = conf.SystemPrompt
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.reset", conf.Lang)))

			case "stop":
				if userStats.CurrentStream != nil {
					userStats.CurrentStream.Close()
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.stop", conf.Lang)))
				} else {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.stop_err", conf.Lang)))
				}

			case "pirdun":
				args := update.Message.CommandArguments()
				if args == "" {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("commands.pirdun", conf.Lang)))
					continue
				}
				fakeMsg := *update.Message
				fakeMsg.Text = args
				go func() {
					if userStats.HaveAccess(conf) {
						responseID := api.HandleChatGPTStreamResponse(bot, client, &fakeMsg, conf, userStats)
						if conf.Model.Type == "openrouter" {
							userStats.GetUsageFromApi(responseID, conf)
						}
					} else {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("budget_out", conf.Lang)))
					}
				}()

			case "get_models", "set_model", "stats":
				// Эти команды уже проверены выше на роль admin — просто выполняем оригинальный код
				switch cmd {
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

				case "stats":
					userStats.CheckHistory(conf.MaxHistorySize, conf.MaxHistoryTime)
					counted := strconv.FormatFloat(userStats.GetCurrentCost(conf.BudgetPeriod), 'f', 6, 64)
					daily := strconv.FormatFloat(userStats.GetCurrentCost("daily"), 'f', 6, 64)
					monthly := strconv.FormatFloat(userStats.GetCurrentCost("monthly"), 'f', 6, 64)
					total := strconv.FormatFloat(userStats.GetCurrentCost("total"), 'f', 6, 64)
					msgs := strconv.Itoa(len(userStats.GetMessages()))
					text := fmt.Sprintf(lang.Translate("commands.stats", conf.Lang), counted, daily, monthly, total, msgs)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
					msg.ParseMode = "HTML"
					bot.Send(msg)
				}

			default:
				// Неизвестная команда — ничего не делаем
			}
		} else {
			// Обычное сообщение (не команда)
			go func() {
				if userStats.HaveAccess(conf) {
					responseID := api.HandleChatGPTStreamResponse(bot, client, update.Message, conf, userStats)
					if conf.Model.Type == "openrouter" {
						userStats.GetUsageFromApi(responseID, conf)
					}
				} else {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, lang.Translate("budget_out", conf.Lang)))
				}
			}()
		}
	}
}