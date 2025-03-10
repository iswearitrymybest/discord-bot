package main

import (
	"DiscordBotIswearitrymybest/internal/config"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Храним созданные временные каналы: ключ – ID канала, значение – ID гильдии
var tempChannels = make(map[string]string)

func main() {
	cfg := config.MustLoad()
	log.Printf("Конфигурация загружена. JoinChannelID: %s", cfg.JoinChannelID)

	// Создаем новую сессию Discord
	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		log.Fatalf("Ошибка при создании сессии: %v", err)
	}

	// Добавляем обработчик событий голосового состояния
	dg.AddHandler(voiceStateUpdate)

	// Включаем необходимые intents
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	// Открываем websocket соединение с Discord
	err = dg.Open()
	if err != nil {
		log.Fatalf("Ошибка при открытии соединения: %v", err)
	}
	log.Println("Бот запущен и подключен к Discord. Ожидание событий...")

	// Запускаем горутину для мониторинга временных каналов
	go monitorTempChannels(dg)

	// Ожидаем завершения работы (CTRL-C)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Println("Получен сигнал завершения, закрываем соединение...")
	if err = dg.Close(); err != nil {
		log.Printf("Ошибка при закрытии соединения: %v", err)
	}
}

// Обработчик обновления голосового состояния
func voiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	cfg := config.MustLoad()
	log.Printf("Получено обновление голосового состояния: UserID=%s, GuildID=%s, ChannelID=%s", vs.UserID, vs.GuildID, vs.ChannelID)
	// Если пользователь зашёл в канал для создания временного
	if vs.ChannelID == cfg.JoinChannelID {
		log.Printf("Пользователь %s вошёл в канал для создания временного канала", vs.UserID)
		guildID := vs.GuildID

		// Формируем имя нового канала
		channelName := fmt.Sprintf("Временный канал - %s", vs.UserID)
		newChannel, err := s.GuildChannelCreate(guildID, channelName, discordgo.ChannelTypeGuildVoice)
		if err != nil {
			log.Printf("Ошибка при создании голосового канала для пользователя %s: %v", vs.UserID, err)
			return
		}
		log.Printf("Создан временный канал: %s для пользователя %s", newChannel.ID, vs.UserID)

		// Сохраняем ID созданного канала
		tempChannels[newChannel.ID] = guildID

		// Перемещаем пользователя в новый канал
		err = s.GuildMemberMove(guildID, vs.UserID, &newChannel.ID)
		if err != nil {
			log.Printf("Ошибка при перемещении пользователя %s в канал %s: %v", vs.UserID, newChannel.ID, err)
			return
		}
		log.Printf("Пользователь %s перемещен в новый канал %s", vs.UserID, newChannel.ID)
	}
}

// Горутина для периодической проверки временных каналов
func monitorTempChannels(s *discordgo.Session) {
	cfg := config.MustLoad()
	log.Println("Запущена горутина для мониторинга временных каналов")
	for {
		time.Sleep(10 * time.Second)
		for channelID, guildID := range tempChannels {
			log.Printf("Проверка состояния канала %s в гильдии %s", channelID, guildID)
			// Получаем информацию о канале
			channel, err := s.Channel(channelID)
			if err != nil {
				log.Printf("Не удалось получить информацию о канале %s: %v. Удаляю его из списка.", channelID, err)
				delete(tempChannels, channelID)
				continue
			}

			// Если канал не голосовой или это основной канал, пропускаем
			if channel.Type != discordgo.ChannelTypeGuildVoice || channelID == cfg.JoinChannelID {
				log.Printf("Канал %s пропущен: не голосовой или основной канал.", channelID)
				continue
			}

			// Получаем гильдию из состояния бота
			guild, err := s.State.Guild(guildID)
			if err != nil {
				log.Printf("Не удалось получить состояние гильдии %s: %v", guildID, err)
				continue
			}

			// Проверяем, есть ли участники в данном голосовом канале
			empty := true
			for _, vs := range guild.VoiceStates {
				if vs.ChannelID == channelID {
					empty = false
					log.Printf("Канал %s занят пользователем %s", channelID, vs.UserID)
					break
				}
			}

			// Если канал пустой, удаляем его
			if empty {
				log.Printf("Канал %s пуст, приступаем к удалению", channelID)
				_, err = s.ChannelDelete(channelID)
				if err != nil {
					log.Printf("Ошибка при удалении временного канала %s: %v", channelID, err)
				} else {
					log.Printf("Временный канал %s успешно удален", channelID)
					delete(tempChannels, channelID)
				}
			}
		}
	}
}
