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
// === НОВАЯ ФУНКЦИЯ: определяем роль пользователя ===
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

// === ДИНАМИЧЕСКОЕ МЕНЮ КОМАНД ===
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
			{Command: "пирдун", Description: "Задать вопрос модели"},
		}
	}

	if role == "user" {
		return []tgbotapi.BotCommand{
			{Command: "start", Description: lang.Translate("description.start", conf.Lang)},
			{Command: "help", Description: "Помощь по доступным командам"},
			{Command: "reset", Description: "Очистить историю разговора"},
			{Command: "stop", Description: "Остановить генерацию"},
			{Command: "пирдун", Description: "Задать вопрос модели"},
		}
	}

	// guest — только минимум
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
	
		userID := update.Message.From.ID
		userStats := userManager.GetUser(userID, update.SentFrom().UserName, conf)
		role := getUserRole(userID, conf)
	
		// Устанавливаем персональное меню команд (только при первом сообщении — можно оптимизировать, но и так ок)
		go bot.Request(tgbotapi.NewSetMyCommands(getBotCommands(userID, conf)...))
	
		// === Если гость и не /start и не /пирдун и не обычное сообщение — блокируем ===
		if role == "guest" {
			if update.Message.IsCommand() {
				cmd := update.Message.Command()
				if cmd != "start" && cmd != "пирдун" {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет доступа к командам. Обратитесь к администратору.")
					bot.Send(msg)
					continue
				}
			}
		}
	
		if update.Message.IsCommand() {
			cmd := update.Message.Command()
		
			// Команды, доступные только админам
			adminOnlyCommands := map[string]bool{
				"get_models": true,
				"set_model":  true,
				"stats":	  true,
			}
		
			if adminOnlyCommands[cmd] && role != "admin" {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Эта команда доступна только администраторам.")
				bot.Send(msg)
				continue
			}
		
			switch cmd {
			
			case "start":
				msgText := lang.Translate("commands.start", conf.Lang)
				if role == "user" || role == "admin" {
					msgText += "\n\nДоступные команды:\n/start — начать\n/пирдун <вопрос> — задать вопрос\n/reset — очистить историю\n/stop — остановить генерацию\n/help — помощь"
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
				msg.ParseMode = "HTML"
				bot.Send(msg)
			
			case "help":
				if role == "guest" {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "У вас ограниченный доступ."))
					continue
				}
				helpText := "<b>Доступные команды:</b>\n\n" +
					"/пирдун &lt;вопрос&gt; — задать вопрос модели\n" +
					"/reset — очистить историю разговора\n" +
					"/stop — прервать текущую генерацию\n" +
					"/help — эта справка"
				if role == "admin" {
					helpText = lang.Translate("commands.help", conf.Lang) // полный хелп для админа
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpText)
				msg.ParseMode = "HTML"
				bot.Send(msg)
			
			case "reset":
				userStats.ClearHistory()
				userStats.SystemPrompt = conf.SystemPrompt // на всякий случай сбрасываем и промпт
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "История очищена ✅")
				bot.Send(msg)
			
			case "stop":
				if userStats.CurrentStream != nil {
					userStats.CurrentStream.Close()
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Генерация остановлена."))
				} else {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Нечего останавливать."))
				}
			
			case "пирдун":
				args := update.Message.CommandArguments()
				if args == "" {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Использование: /пирдун <текст запроса>"))
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
				
			// === Админские команды (get_models, set_model, stats) ===
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
				countedUsage := strconv.FormatFloat(userStats.GetCurrentCost(conf.BudgetPeriod), 'f', 6, 64)
				todayUsage := strconv.FormatFloat(userStats.GetCurrentCost("daily"), 'f', 6, 64)
				monthUsage := strconv.FormatFloat(userStats.GetCurrentCost("monthly"), 'f', 6, 64)
				totalUsage := strconv.FormatFloat(userStats.GetCurrentCost("total"), 'f', 6, 64)
				messagesCount := strconv.Itoa(len(userStats.GetMessages()))
				statsMessage := fmt.Sprintf(lang.Translate("commands.stats", conf.Lang), countedUsage, todayUsage, monthUsage, totalUsage, messagesCount)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, statsMessage)
				msg.ParseMode = "HTML"
				bot.Send(msg)
		} else {
			// Обычные сообщения — только если есть доступ по бюджету
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
