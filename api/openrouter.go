package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"openrouter-bot/config"
	configs "openrouter-bot/config"
	"openrouter-bot/lang"
	"openrouter-bot/user"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/sashabaranov/go-openai"
)

type Model struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Pricing     struct {
		Prompt string `json:"prompt"`
	} `json:"pricing"`
}

type APIResponse struct {
	Data []Model `json:"data"`
}

func GetFreeModels() (string, error) {
	manager, err := config.NewManager("./config.yaml")
	if err != nil {
		log.Fatalf("Error initializing config manager: %v", err)
	}
	conf := manager.GetConfig()

	resp, err := http.Get(conf.OpenAIBaseURL + "/models")
	if err != nil {
		return "", fmt.Errorf("error get models: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error read response: %v", err)
	}

	var apiResponse APIResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parse json: %v", err)
	}

	var result strings.Builder
	for _, model := range apiResponse.Data {
		// Filter by price
		if model.Pricing.Prompt == "0" {
			// escapedDesc := strings.ReplaceAll(model.Description, "*", "\\*")
			// escapedDesc = strings.ReplaceAll(escapedDesc, "_", "\\_")
			// result.WriteString(fmt.Sprintf("%s - %s\n", model.ID, escapedDesc))
			result.WriteString(fmt.Sprintf("➡ `%s`\n", model.ID))
			// result.WriteString(fmt.Sprintf("➡ `/set_model %s`\n", model.ID))
		}
	}
	return result.String(), nil
}

func HandleChatGPTStreamResponse(
	bot *tgbotapi.BotAPI,
	client *openai.Client,
	message *tgbotapi.Message,
	config *config.Config,
	user *user.UsageTracker,
) string {

	ctx := context.Background()
	user.CheckHistory(config.MaxHistorySize, config.MaxHistoryTime)
	user.LastMessageTime = time.Now()

	err := lang.LoadTranslations("./lang/")
	if err != nil {
		log.Fatalf("Error loading translations: %v", err)
	}

	manager, err := configs.NewManager("./config.yaml")
	if err != nil {
		log.Fatalf("Error initializing config manager: %v", err)
	}
	conf := manager.GetConfig()

	errorMessage := lang.Translate("errorText", conf.Lang)

	// Формируем историю сообщений
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: user.SystemPrompt,
		},
	}

	for _, msg := range user.GetMessages() {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	if config.Vision == "true" {
		messages = append(messages, addVisionMessage(bot, message, config))
	} else {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: message.Text,
		})
	}

	// Готовый нестримовый запрос
	req := openai.ChatCompletionRequest{
		Model:            config.Model.ModelName,
		FrequencyPenalty: float32(config.Model.FrequencyPenalty),
		PresencePenalty:  float32(config.Model.PresencePenalty),
		Temperature:      float32(config.Model.Temperature),
		TopP:             float32(config.Model.TopP),
		MaxTokens:        config.MaxTokens,
		Messages:         messages,
		Stream:           false,
	}

	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		log.Printf("ChatCompletion error: %v\n", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, errorMessage)
		bot.Send(msg)
		return ""
	}

	answer := resp.Choices[0].Message.Content

	// Записываем в историю
	user.AddMessage(openai.ChatMessageRoleUser, message.Text)
	user.AddMessage(openai.ChatMessageRoleAssistant, answer)

	// Отправляем ответ одним сообщением
	msg := tgbotapi.NewMessage(message.Chat.ID, answer)
	msg.ParseMode = tgbotapi.ModeMarkdown
	bot.Send(msg)

	return resp.ID
}

func addVisionMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message, config *config.Config) openai.ChatCompletionMessage {
	if len(message.Photo) > 0 {
		// Assuming you want the largest photo size
		photoSize := message.Photo[len(message.Photo)-1]
		fileID := photoSize.FileID

		// Download the photo
		file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
		if err != nil {
			log.Printf("Error getting file: %v", err)
			return openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: message.Text,
			}
		}

		// Access the file URL
		fileURL := file.Link(bot.Token)
		fmt.Println("Photo URL:", fileURL)
		if message.Text == "" {
			message.Text = config.VisionPrompt
		}

		return openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleUser,
			MultiContent: []openai.ChatMessagePart{
				{
					Type: openai.ChatMessagePartTypeText,
					Text: message.Text,
				},
				{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL:    fileURL,
						Detail: openai.ImageURLDetail(config.VisionDetails),
					},
				},
			},
		}
	} else {
		return openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: message.Text,
		}
	}
}
