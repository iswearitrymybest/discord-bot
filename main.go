package main

import (
	"DiscordBotIswearitrymybest/internal/config"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Замените на ваш токен бота
const BotToken = "MTM0ODU3MDAzNTM0MTIzNDE4Nw.GMaKW2.74WNj65pTnC1GtoiqP-9YMg9aq2M3MKcZqzZYc"

// ID голосового канала, в который пользователи заходят для создания временного канала
const JoinChannelID = "906124525492641828"

// Храним созданные временные каналы: ключ – ID канала, значение – ID гильдии
var tempChannels = make(map[string]string)

func main() {
	cfg := config.MustLoad()

	// Создаем новую сессию Discord
	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		fmt.Println("Ошибка при создании сессии:", err)
		return
	}

	// Добавляем обработчик событий голосового состояния
	dg.AddHandler(voiceStateUpdate)

	// Включаем необходимые intents
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	// Открываем websocket соединение с Discord
	err = dg.Open()
	if err != nil {
		fmt.Println("Ошибка при открытии соединения:", err)
		return
	}
	fmt.Println("Бот запущен. Для выхода нажмите CTRL-C.")

	// Запускаем горутину для мониторинга временных каналов
	go monitorTempChannels(dg)

	// Ожидаем завершения работы (CTRL-C)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

// Обработчик обновления голосового состояния
func voiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	cfg := config.MustLoad()
	// Если пользователь зашёл в канал для создания временного
	if vs.ChannelID == cfg.JoinChannelID {
		guildID := vs.GuildID

		// Формируем имя нового канала (можно изменить по желанию)
		channelName := fmt.Sprintf("Временный канал - %s", vs.UserID)
		newChannel, err := s.GuildChannelCreate(guildID, channelName, discordgo.ChannelTypeGuildVoice)
		if err != nil {
			fmt.Println("Ошибка при создании голосового канала:", err)
			return
		}
		fmt.Println("Создан временный канал:", newChannel.ID)

		// Сохраняем ID созданного канала
		tempChannels[newChannel.ID] = guildID

		// Перемещаем пользователя в новый канал
		// Для этого бот должен иметь право перемещать участников (MOVE_MEMBERS)
		err = s.GuildMemberMove(guildID, vs.UserID, &newChannel.ID)
		if err != nil {
			fmt.Println("Ошибка при перемещении пользователя:", err)
			return
		}
	}
}

// Горутина для периодической проверки временных каналов
func monitorTempChannels(s *discordgo.Session) {
	cfg := config.MustLoad()
	for {
		time.Sleep(10 * time.Second)
		for channelID, guildID := range tempChannels {
			// Получаем информацию о канале
			channel, err := s.Channel(channelID)
			if err != nil {
				// Если канал уже удалён, удаляем его из мапы
				delete(tempChannels, channelID)
				continue
			}

			// Если канал не голосовой или это основной канал, пропускаем
			if channel.Type != discordgo.ChannelTypeGuildVoice || channelID == cfg.JoinChannelID {
				continue
			}

			// Получаем гильдию из состояния бота
			guild, err := s.State.Guild(guildID)
			if err != nil {
				continue
			}

			// Проверяем, есть ли участники в данном голосовом канале
			empty := true
			for _, vs := range guild.VoiceStates {
				if vs.ChannelID == channelID {
					empty = false
					break
				}
			}

			// Если канал пустой, удаляем его
			if empty {
				_, err = s.ChannelDelete(channelID)
				if err != nil {
					fmt.Println("Ошибка при удалении временного канала:", err)
				} else {
					fmt.Println("Удалён временный канал:", channelID)
					delete(tempChannels, channelID)
				}
			}
		}
	}
}
