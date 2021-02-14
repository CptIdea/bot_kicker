package main

import (
	"encoding/json"
	"fmt"
	vk "github.com/CptIdea/go-vk-api-2"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var GROUPID = GROUP ID HERE
var votes = make(map[int]*Voting)
var config struct {
	Secs int
	Sups []int
	Min  int `json:"min_to_kick"`
	Days int `json:"min_days_in_chat"`
}

func main() {
	bot := vk.NewSession("TOKEN","5.110")


main:
	for {
		update,err := bot.UpdateCheck(GROUPID)
		if err != nil {
			fmt.Println(err)
			continue
		}
		for _, update := range update.Updates {
			UpdateConfig()

			var err error
			resp,err := bot.SendRequest("messages.getConversationMembers", vk.Request{"peer_id": update.Object.MessageNew.PeerId, "group_id": GROUPID})
			if err != nil {
				fmt.Println(err)
				continue
			}
			Chat := struct {
				Response struct {
					Items []struct {
						Id        int `json:"member_id"`
						Is_admin  bool
						Join_date int
					}
				}
			}{}
			err = json.Unmarshal(resp, &Chat)
			if err != nil {
				fmt.Println(err)
				continue
			}
			for _, profile := range Chat.Response.Items {
				if time.Since(time.Unix(int64(profile.Join_date), 0)) < 2*time.Hour*24*time.Duration(config.Days) && profile.Id == update.Object.MessageNew.FromId && !isSup(update.Object.MessageNew.FromId) {
					continue main
				}
			}

			if update.Object.MessageNew.Text == "!fuck" {
				KickID := 0
				if update.Object.MessageNew.ReplyMessage != nil {
					KickID = update.Object.MessageNew.ReplyMessage.FromId
				}

				if KickID <= 0 {
					if len(update.Object.MessageNew.FwdMessages) > 0 {
						KickID = update.Object.MessageNew.FwdMessages[0].FromId
					}
					if KickID <= 0 {
						bot.SendMessage(update.Object.MessageNew.PeerId, "Пожалуйста, ответьте на сообщение пользователя, которого хотите кикнуть")
						continue
					}
				}

				if _, ok := votes[KickID]; ok {
					msg := "Голосование уже начато."
					if len(votes[KickID].voters) > 0 {
						msg += " Список голосов:\n"
						for _, v := range votes[KickID].voters {
							msg += v.FirstName + " " + v.LastName + " - "
							if v.vote {
								msg += "За"
							} else {
								msg += "Против"
							}
							if isSup(v.ID) {
								msg += " x2"
							}
							msg += "\n"
						}
					} else {
						msg += " Голосов пока нет..."
					}
					bot.SendMessage(update.Object.MessageNew.PeerId, msg)
					continue
				}

				users,_ := bot.GetUsersInfo([]int{KickID})
				user := users[0]
				bot.SendKeyboard(update.Object.MessageNew.PeerId, GetKeyboard(KickID), "Запущено голосование для кика пользователя [id"+strconv.Itoa(KickID)+"|"+user.FirstName+" "+user.LastName+"]\n"+"Время на голосование: "+strconv.Itoa(config.Secs)+" секунд")


				Voting := Voting{
					chat:     update.Object.MessageNew.PeerId,
					timer:    time.NewTimer(time.Duration(config.Secs) * time.Second),
					kickUser: user,
					votes:    make(chan voter),
					voters:   make(map[int]voter),
					cancel:   make(chan bool),
					author:   update.Object.MessageNew.FromId,
				}
				go VoteControl(&Voting, bot)
				continue
			}

			if update.Object.MessageNew.Payload != "" {
				curKick, _ := strconv.Atoi(update.Object.MessageNew.Payload)
				if _, ok := votes[curKick]; ok {
					if strings.Contains(update.Object.MessageNew.Text, "Да") {
						usrs, _ := bot.GetUsersInfo([]int{update.Object.MessageNew.FromId})
						usr := usrs[0]
						votes[curKick].votes <- voter{
							User: usr,
							vote: true,
						}
					}
					if strings.Contains(update.Object.MessageNew.Text, "Нет") {
						usrs, _ := bot.GetUsersInfo([]int{update.Object.MessageNew.FromId})
						usr := usrs[0]
						votes[curKick].votes <- voter{
							User: usr,
							vote: false,
						}
					}
					if strings.Contains(update.Object.MessageNew.Text, "Отмена") {
						if votes[curKick].author == update.Object.MessageNew.FromId {
							fmt.Println("author")
							votes[curKick].cancel <- true
							continue main
						}
						//for _, sup := range config.Sups {
						//	if update.Object.MessageNew.FromId == sup {
						//		fmt.Println("sup")
						//		votes[curKick].cancel <- true
						//		continue main
						//	}
						//}
						for _, p := range Chat.Response.Items {
							if p.Is_admin && p.Id == update.Object.MessageNew.FromId {
								fmt.Println("admin")
								votes[curKick].cancel <- true
								continue main
							}
						}
						bot.SendMessage(update.Object.MessageNew.PeerId, "Вы не можете отменить голосование")
					}
				}
			}
		}
	}
}

type Voting struct {
	chat     int
	kickUser vk.User
	timer    *time.Timer
	voters   map[int]voter
	votes    chan voter
	cancel   chan bool
	author   int
}

type voter struct {
	vk.User
	vote bool
}

func VoteControl(voting *Voting, bot vk.Session) {
	votes[voting.kickUser.ID] = voting
	fmt.Println("Создано голосование")
	for {
		select {
		case <-voting.cancel:
			delete(votes, voting.kickUser.ID)
			bot.SendMessage(voting.chat, "Отмена голосования по кику "+voting.kickUser.FirstName+" "+voting.kickUser.LastName)
			fmt.Println("Голосование+" + voting.String() + " закончено. Отмена")
			return
		case <-voting.timer.C:
			fmt.Println("Голосование+" + voting.String() + " закончено. Таймер")
			if len(voting.voters) < config.Min {
				bot.SendMessage(voting.chat, fmt.Sprintf("Недостаточно голосов чтобы принять решение по кику %s %s\n%d/%d", voting.kickUser.FirstName, voting.kickUser.LastName, len(voting.voters), config.Min))
				delete(votes, voting.kickUser.ID)
				return
			}
			delete(votes, voting.kickUser.ID)
			kick := 0

			for _, v := range voting.voters {
				if v.vote {
					kick++
					for _, sup := range config.Sups {
						if sup == v.ID {
							kick++
							break
						}
					}
				}
			}
			if kick > len(voting.voters)/2 {
				bot.SendMessage(voting.chat, "Скажите пока-пока пользователю "+voting.kickUser.FirstName+" "+voting.kickUser.LastName)
				bot.SendRequest("messages.removeChatUser", vk.Request{"chat_id": voting.chat - 2000000000, "member_id": voting.kickUser.ID})
			} else {
				bot.SendMessage(voting.chat, "Не хватает голосов чтобы кикнуть "+voting.kickUser.FirstName+" "+voting.kickUser.LastName+"\nГолосов: "+strconv.Itoa(kick)+"/"+strconv.Itoa(len(voting.voters)/2))
			}


		case voter := <-voting.votes:
			ans, ok := voting.voters[voter.ID]
			if !ok {
				if voter.vote {
					bot.SendMessage(voting.chat, "Принят голос \"За\" в голосовании по "+voting.kickUser.FirstName+" "+voting.kickUser.LastName)
				} else {
					bot.SendMessage(voting.chat, "Принят голос \"Против\" в голосовании по "+voting.kickUser.FirstName+" "+voting.kickUser.LastName)
				}
			} else {
				if voter.vote != ans.vote {
					bot.SendMessage(voting.chat, "Заменён голос в голосовании по "+voting.kickUser.FirstName+" "+voting.kickUser.LastName)
				}
			}
			voting.voters[voter.ID] = voter
			fmt.Printf("Голосование %s %s: голос %s %s - %t\n", voting.kickUser.FirstName, voting.kickUser.LastName, voter.FirstName, voter.LastName, voter.vote)
		}
	}
}

func GetKeyboard(id int) vk.Keyboard {
	YNKeyboard := vk.Keyboard{
		Inline:  true,
		Buttons: [][]vk.Button{make([]vk.Button, 1), make([]vk.Button, 1), make([]vk.Button, 1)},
	}
	YNKeyboard.Buttons[0][0].Action.Type = "text"
	YNKeyboard.Buttons[0][0].Action.Label = "Да"
	YNKeyboard.Buttons[0][0].Color = "positive"
	YNKeyboard.Buttons[0][0].Action.Payload = strconv.Itoa(id)

	YNKeyboard.Buttons[1][0].Action.Type = "text"
	YNKeyboard.Buttons[1][0].Action.Label = "Нет"
	YNKeyboard.Buttons[1][0].Color = "negative"
	YNKeyboard.Buttons[1][0].Action.Payload = strconv.Itoa(id)

	YNKeyboard.Buttons[2][0].Action.Type = "text"
	YNKeyboard.Buttons[2][0].Action.Label = "Отмена"
	YNKeyboard.Buttons[2][0].Color = "secondary"
	YNKeyboard.Buttons[2][0].Action.Payload = strconv.Itoa(id)
	return YNKeyboard
}

func (v Voting) String() string {
	return "{" + strconv.Itoa(v.chat) + " " + strconv.Itoa(v.kickUser.ID) + "}"
}

func UpdateConfig() {
	cnfgResp, err := http.Get("https://raw.githubusercontent.com/dima13230/bot_kicker/main/config.json")
	if err != nil {
		fmt.Println(err)
	}
	cnfgFile, err := ioutil.ReadAll(cnfgResp.Body)
	//cnfgFile, err := ioutil.ReadFile("config.json")
	if err != nil {
		fmt.Println(err)
	}

	err = json.Unmarshal(cnfgFile, &config)
	if err != nil {
		fmt.Println(err)
	}
}

func isSup(id int) bool {
	for _, sup := range config.Sups {
		if sup == id {
			return true
		}
	}
	return false
}
