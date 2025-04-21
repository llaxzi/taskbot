package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	storageP "github.com/llaxzi/taskbot/storage"
	tgbotapi "github.com/skinass/telegram-bot-api/v5"
)

var (

	// @BotFather в телеграме даст вам это
	BotToken   string
	WebhookURL string

	runAddr        string
	webhookPattern string

	storage = storageP.NewStorage()

	errWrongFormat = "Неверный формат команды."
	errNoTasks     = "Нет задач"
)

func flagParse() {
	flag.StringVar(&BotToken, "token", "", "token for telegram")
	flag.StringVar(&WebhookURL, "webhook", "", "webhook addr for telegram")
	flag.StringVar(&runAddr, "runAddr", ":8080", "tg bot addr")
	flag.StringVar(&webhookPattern, "webhookPattern", "/", "webhook url pattern")
	flag.Parse()

	// Обеспечиваем работоспособность тестов
	if WebhookURL == "" {
		WebhookURL = fmt.Sprintf("http://127.0.0.1%s", runAddr)
	}

}

func main() {
	err := startTaskBot(context.Background())
	if err != nil {
		panic(err)
	}
}

func startTaskBot(ctx context.Context) error {
	flagParse() // Не в main ради тестов
	bot, err := tgbotapi.NewBotAPI(BotToken)
	if err != nil {
		return fmt.Errorf("failed to start taskBot: %w", err)
	}
	log.Printf("Authorized as @%s", bot.Self.UserName)

	// Устанавливаем webhook на URL
	wh, err := tgbotapi.NewWebhook(WebhookURL)
	if err != nil {
		return fmt.Errorf("NewWebhook failed: %w", err)
	}
	_, err = bot.Request(wh)
	if err != nil {
		return fmt.Errorf("SetWebhook failed: %w", err)
	}

	// Telegram будет стучаться сюда
	updates := bot.ListenForWebhook(webhookPattern)

	// Поднимаем HTTP-сервер
	go func() {
		if err = http.ListenAndServe(runAddr, nil); err != nil {
			log.Fatal(err)
		}
	}()
	log.Printf("Bot is listening on %s on %s\n", runAddr, webhookPattern)

	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			handleUpdate(bot, update)
		}
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	username := update.Message.From.UserName
	msg := update.Message.Text

	parts := strings.SplitN(msg, " ", 2)
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}

	switch {
	case cmd == "/tasks":
		sendMsg(bot, chatID, buildTasks(userID))
	case cmd == "/new":
		if arg == "" {
			sendMsg(bot, chatID, errWrongFormat)
			return
		}
		id := storage.NewTask(arg, userID, username)
		sendMsg(bot, chatID, fmt.Sprintf("Задача \"%s\" создана, id=%d", arg, id))
	case strings.HasPrefix(cmd, "/assign_"):
		taskID, err := parseTaskID(cmd, "/assign_")
		if err != nil {
			sendMsg(bot, chatID, errWrongFormat)
			return
		}
		handleAssign(bot, chatID, userID, taskID, username)
	case strings.HasPrefix(cmd, "/unassign_"):
		taskID, err := parseTaskID(cmd, "/unassign_")
		if err != nil {
			sendMsg(bot, chatID, errWrongFormat)
			return
		}
		handleUnassign(bot, chatID, userID, taskID)
	case strings.HasPrefix(cmd, "/resolve_"):
		taskID, err := parseTaskID(cmd, "/resolve_")
		if err != nil {
			sendMsg(bot, chatID, errWrongFormat)
			return
		}
		handleResolve(bot, chatID, userID, taskID, username)
	case cmd == "/my":
		sendMsg(bot, chatID, buildMyTasks(userID))
	case cmd == "/owner":
		sendMsg(bot, chatID, buildOwnedTasks(userID))
	default:
		sendMsg(bot, chatID, errWrongFormat)
	}

}

func handleAssign(bot *tgbotapi.BotAPI, chatID, userID, taskID int64, username string) {
	res := storage.AssignTask(taskID, userID, username)
	if res.Err != nil {
		sendMsg(bot, chatID, res.Err.Error())
		return
	}
	sendMsg(bot, chatID, fmt.Sprintf("Задача \"%s\" назначена на вас", res.Name))
	if *res.OldAssigneeID != 0 && *res.OldAssigneeID != userID {
		sendMsg(bot, *res.OldAssigneeID, fmt.Sprintf("Задача \"%s\" назначена на @%s", res.Name, username))
		return
	}
	if res.OwnerID != userID {
		sendMsg(bot, res.OwnerID, fmt.Sprintf("Задача \"%s\" назначена на @%s", res.Name, username))
	}
}

func handleUnassign(bot *tgbotapi.BotAPI, chatID, userID, taskID int64) {
	res := storage.UnassignTask(taskID, userID)
	if res.Err != nil {
		sendMsg(bot, chatID, res.Err.Error())
		return
	}
	sendMsg(bot, chatID, "Принято")
	sendMsg(bot, res.OwnerID, fmt.Sprintf("Задача \"%s\" осталась без исполнителя", res.Name))
}

func handleResolve(bot *tgbotapi.BotAPI, chatID, userID, taskID int64, username string) {
	res := storage.ResolveTask(taskID, userID)
	if res.Err != nil {
		sendMsg(bot, chatID, res.Err.Error())
		return
	}
	sendMsg(bot, chatID, fmt.Sprintf("Задача \"%s\" выполнена", res.Name))
	if res.OwnerID != userID {
		sendMsg(bot, res.OwnerID, fmt.Sprintf("Задача \"%s\" выполнена @%s", res.Name, username))
	}
}

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	_, err := bot.Send(tgbotapi.NewMessage(chatID, text))
	if err != nil {
		log.Printf("sendMsg error: %v", err)
	}
}

func buildTasks(userID int64) string {
	tasks := storage.AllTasks()
	if len(tasks) == 0 {
		return errNoTasks
	}

	// Сортируем, тк. storage - map
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID() < tasks[j].ID()
	})

	var sb strings.Builder
	for i, task := range tasks {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		info := task.Info()

		sb.WriteString(fmt.Sprintf("%d. %s by @%s\n", info.ID, info.Name, info.OwnerUsername))
		switch {
		case !info.Assigned:
			sb.WriteString(fmt.Sprintf("/assign_%d", task.ID()))
		case info.AssigneeID == userID:
			sb.WriteString("assignee: я\n")
			sb.WriteString(fmt.Sprintf("/unassign_%d /resolve_%d", task.ID(), task.ID()))
		default:
			sb.WriteString(fmt.Sprintf("assignee: @%s", info.AssigneeUsername))
		}

	}
	return sb.String()
}

func parseTaskID(cmd, prefix string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(cmd, prefix), 10, 64)
}

func buildMyTasks(userID int64) string {
	tasks := storage.AssignedTasks(userID)
	if len(tasks) == 0 {
		return errNoTasks
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID() < tasks[j].ID()
	})

	var sb strings.Builder
	for i, task := range tasks {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		info := task.Info()

		sb.WriteString(fmt.Sprintf("%d. %s by @%s\n", info.ID, info.Name, info.OwnerUsername))
		sb.WriteString(fmt.Sprintf("/unassign_%d /resolve_%d", info.ID, info.ID))
	}

	return sb.String()
}

func buildOwnedTasks(userID int64) string {
	tasks := storage.OwnedTasks(userID)
	if len(tasks) == 0 {
		return errNoTasks
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID() < tasks[j].ID()
	})

	var sb strings.Builder
	for i, task := range tasks {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		info := task.Info()

		sb.WriteString(fmt.Sprintf("%d. %s by @%s\n", info.ID, info.Name, info.OwnerUsername))
		sb.WriteString(fmt.Sprintf("/assign_%d", info.ID))
	}

	return sb.String()
}
