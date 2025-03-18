package main

import (
	"DiscordBotIswearitrymybest/handler"
	"DiscordBotIswearitrymybest/internal/config"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

func main() {
	cfg := config.MustLoad()

	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		log.Fatalf("Ошибка создания сессии: %v", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	dg.AddHandler(func(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
		handler.VoiceStateHandler(s, vs, cfg)
	})

	err = dg.Open()
	if err != nil {
		log.Fatalf("Ошибка открытия соединения: %v", err)
	}
	log.Println("Бот запущен и подключен к Discord. Ожидание событий...")

	go handler.MonitorTempChannels(dg, cfg)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("Получен сигнал завершения, закрываем соединение...")
	dg.Close()
}
