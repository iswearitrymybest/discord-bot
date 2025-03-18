package handler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"DiscordBotIswearitrymybest/internal/config"

	"github.com/bwmarrin/discordgo"
)

// TempChannelInfo хранит информацию о созданном временном голосовом канале.
type TempChannelInfo struct {
	GuildID   string
	Number    int
	CreatedAt time.Time
}

var (
	// tempChannels: ключ – ID временного канала, значение – информация о канале.
	tempChannels = make(map[string]TempChannelInfo)
	// channelNumbers: для каждой гильдии хранится, какие номера уже заняты.
	channelNumbers = make(map[string]map[int]bool)
	mu             sync.Mutex
)

// voiceStateHandler обрабатывает события обновления голосового состояния.
func VoiceStateHandler(s *discordgo.Session, vs *discordgo.VoiceStateUpdate, cfg *config.Config) {
	// Логируем подробную информацию для отладки.
	log.Printf("VoiceStateUpdate: UserID=%s, GuildID=%s, ChannelID=%q, BeforeUpdate=%+v",
		vs.UserID, vs.GuildID, vs.ChannelID, vs.BeforeUpdate)

	// Если пользователь вошёл в канал для создания временных каналов, то его ChannelID должен совпадать с JoinChannelID.
	if vs.ChannelID != cfg.JoinChannelID {
		return
	}

	log.Printf("Пользователь %s вошёл в канал создания (%s)", vs.UserID, cfg.JoinChannelID)
	guildID := vs.GuildID

	// Очистка устаревших записей временных каналов для данной гильдии.
	mu.Lock()
	for chanID, info := range tempChannels {
		if info.GuildID == guildID {
			if _, err := s.Channel(chanID); err != nil {
				log.Printf("Очистка: удаляем устаревший канал %s", chanID)
				delete(tempChannels, chanID)
				if nums, ok := channelNumbers[info.GuildID]; ok {
					delete(nums, info.Number)
				}
			}
		}
	}
	// Подсчёт активных временных каналов.
	count := 0
	for _, info := range tempChannels {
		if info.GuildID == guildID {
			count++
		}
	}
	if count >= cfg.MaxChannels {
		mu.Unlock()
		log.Println("Достигнуто максимальное количество временных каналов!")
		return
	}
	mu.Unlock()

	log.Println("Вызываем getNextChannelNumber...")
	number := GetNextChannelNumber(guildID, cfg.MaxChannels)
	if number == -1 {
		log.Println("Нет доступного номера для создания канала")
		return
	}

	channelName := fmt.Sprintf("Водопойка №%d", number)
	pos := GetChannelPosition(s, guildID, cfg.PositionRefID)

	newChannel, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     channelName,
		Type:     discordgo.ChannelTypeGuildVoice,
		ParentID: cfg.TempParentID,
		Position: pos,
	})
	if err != nil {
		log.Printf("Ошибка создания временного канала: %v", err)
		// Освобождаем номер в случае ошибки.
		mu.Lock()
		if nums, ok := channelNumbers[guildID]; ok {
			delete(nums, number)
		}
		mu.Unlock()
		return
	}
	log.Printf("Создан временный канал: %s (№%d)", newChannel.ID, number)

	// Сохраняем информацию о созданном канале.
	mu.Lock()
	if _, ok := channelNumbers[guildID]; !ok {
		channelNumbers[guildID] = make(map[int]bool)
	}
	channelNumbers[guildID][number] = true
	tempChannels[newChannel.ID] = TempChannelInfo{
		GuildID:   guildID,
		Number:    number,
		CreatedAt: time.Now(),
	}
	mu.Unlock()

	// Перемещаем пользователя в созданный канал.
	err = s.GuildMemberMove(guildID, vs.UserID, &newChannel.ID)
	if err != nil {
		log.Printf("Ошибка перемещения пользователя %s в канал %s: %v", vs.UserID, newChannel.ID, err)
	} else {
		log.Printf("Пользователь %s перемещён в канал %s", vs.UserID, newChannel.ID)
	}
}

// getNextChannelNumber возвращает первый свободный номер для указанной гильдии.
func GetNextChannelNumber(guildID string, maxChannels int) int {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := channelNumbers[guildID]; !ok {
		channelNumbers[guildID] = make(map[int]bool)
	}
	for i := 1; i <= maxChannels; i++ {
		if !channelNumbers[guildID][i] {
			// Отмечаем номер как занятый.
			channelNumbers[guildID][i] = true
			return i
		}
	}
	return -1
}

// getChannelPosition возвращает позицию для нового канала относительно указанного канала.
func GetChannelPosition(s *discordgo.Session, guildID, refChannelID string) int {
	ch, err := s.Channel(refChannelID)
	if err != nil {
		log.Printf("Ошибка получения позиции канала %s: %v", refChannelID, err)
		return 0
	}
	return ch.Position + 1
}

// monitorTempChannels периодически проверяет временные каналы и удаляет пустые, если истёк период защиты.
func MonitorTempChannels(s *discordgo.Session, cfg *config.Config) {
	// Интервал проверки задаём равным 10 секундам.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Преобразуем время защиты из конфигурации (секунды) в duration.
	gracePeriod := time.Duration(cfg.GracePeriod) * time.Second

	for range ticker.C {
		mu.Lock()
		for chanID, info := range tempChannels {
			// Если канал создан недавно – пропускаем проверку.
			if time.Since(info.CreatedAt) < gracePeriod {
				log.Printf("Канал %s находится в grace period", chanID)
				continue
			}

			ch, err := s.Channel(chanID)
			if err != nil {
				log.Printf("Не удалось получить информацию о канале %s: %v. Удаляем запись.", chanID, err)
				delete(tempChannels, chanID)
				if nums, ok := channelNumbers[info.GuildID]; ok {
					delete(nums, info.Number)
				}
				continue
			}

			if ch.Type != discordgo.ChannelTypeGuildVoice {
				continue
			}

			guild, err := s.State.Guild(info.GuildID)
			if err != nil {
				log.Printf("Ошибка получения гильдии %s: %v", info.GuildID, err)
				continue
			}

			empty := true
			for _, vs := range guild.VoiceStates {
				if vs.ChannelID == chanID {
					empty = false
					break
				}
			}

			if empty {
				log.Printf("Канал %s пуст. Удаляем.", chanID)
				_, err = s.ChannelDelete(chanID)
				if err != nil {
					log.Printf("Ошибка удаления канала %s: %v", chanID, err)
				} else {
					log.Printf("Удалён временный канал %s", chanID)
				}
				delete(tempChannels, chanID)
				if nums, ok := channelNumbers[info.GuildID]; ok {
					delete(nums, info.Number)
				}
			}
		}
		log.Printf("Монитор: активных временных каналов – %d", len(tempChannels))
		mu.Unlock()
	}
}
