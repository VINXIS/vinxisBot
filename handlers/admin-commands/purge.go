package admincommands

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	tools "maquiaBot/tools"

	"github.com/bwmarrin/discordgo"
)

// Purge lets admins purge messages including their purge command
func Purge(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Check if server exists
	server, err := s.Guild(m.GuildID)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "This is not a server so custom prefixes are unavailable! Please use `$` instead for commands!")
		return
	}

	if !tools.AdminCheck(s, m, *server) {
		s.ChannelMessageSend(m.ChannelID, "You must be an admin, server manager, or server owner!")
		return
	}

	// Get username(s) and number of messages
	userRegex, _ := regexp.Compile(`(?i)purge\s+(.+)`)
	dateRegex, _ := regexp.Compile(`(?i)since\s+(.+)`)

	userText := ""
	num := 4
	dateTime := time.Time{}
	method := "count"
	var usernames []string

	// See if we use date instead of counting
	if dateRegex.MatchString(m.Content) {
		// Parse date
		date := dateRegex.FindStringSubmatch(m.Content)[1]
		dateTime, err = tools.TimeParse(date)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Invalid datetime format! Error: "+err.Error())
			return
		}

		if dateTime.Year() == 0 {
			dateTime = dateTime.AddDate(time.Now().Year(), 0, 0)
		} else if dateTime.Year() == 1 {
			dateTime = dateTime.AddDate(time.Now().Year()+1, 0, 0)
		}

		method = "date"
		m.Content = strings.TrimSpace(strings.Replace(m.Content, dateRegex.FindStringSubmatch(m.Content)[0], "", -1))
	}

	// Get user (and count)
	if userRegex.MatchString(m.Content) {
		userNum := userRegex.FindStringSubmatch(strings.Replace(m.ContentWithMentionsReplaced(), "@", "", -1))[1]
		args := strings.Split(userNum, " ")
		for _, arg := range args {
			if i, err := strconv.Atoi(arg); err == nil && i > 0 {
				userNum = strings.TrimSpace(strings.Replace(userNum, arg, "", 1))
				num = i + 1
				break
			}
		}
		if userNum != "" {
			usernames = append(usernames, strings.Split(userNum, " ")...)
		}
	}
	if len(usernames) != 0 {
		for _, username := range usernames {
			userText += "**" + username + "** "
		}
	}

	// Get messages
	messages, err := s.ChannelMessages(m.ChannelID, -1, "", "", "")
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "Error obtaining messages!")
		return
	}

	// Get messages and delete them
	var messageIDs []string
	lastID := ""
	prevLength := 0
	recurring := 0
	done := false
	for {
		for _, msg := range messages {
			if method == "date" {
				msgTime, err := msg.Timestamp.Parse()
				if err != nil {
					s.ChannelMessageSend(m.ChannelID, "Somehow an error occured in parsing a message timestamp. Please try again.")
					return
				}
				if dateTime.After(msgTime) {
					done = true
					num = len(messageIDs)
					break
				}
			}
			if len(usernames) == 0 {
				messageIDs = append(messageIDs, msg.ID)
			} else {
				for _, username := range usernames {
					if strings.HasPrefix(strings.ToLower(msg.Author.Username), strings.ToLower(username)) || (msg.Member != nil && strings.HasPrefix(strings.ToLower(msg.Member.Nick), strings.ToLower(username))) {
						messageIDs = append(messageIDs, msg.ID)
						break
					}
				}
			}
			if method == "count" && len(messageIDs) == num {
				done = true
				break
			}
			lastID = msg.ID
		}
		if done {
			break
		}
		if prevLength == len(messageIDs) {
			recurring++
		} else {
			prevLength = len(messageIDs)
			recurring = 1
		}
		if recurring == 5 {
			num = len(messageIDs)
			break
		}
		messages, err = s.ChannelMessages(m.ChannelID, -1, lastID, "", "")
		if err != nil {
			break
		}
	}

	if len(messageIDs) == 0 {
		s.ChannelMessageSend(m.ChannelID, "No messages found with the given usernames: "+userText)
		return
	}

	if len(messageIDs) > 100 {
		i := 100
		for {
			if i >= len(messageIDs) {
				i = len(messageIDs) - 1
			}
			err = s.ChannelMessagesBulkDelete(m.ChannelID, messageIDs[i-100:i])
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Could not delete messages! Please make sure I have the proper permissions!")
				return
			}
			if i == len(messageIDs)-1 {
				break
			}
			i += 100
		}
	} else {
		err = s.ChannelMessagesBulkDelete(m.ChannelID, messageIDs)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Could not delete messages! Please make sure I have the proper permissions!")
			return
		}
	}

	// Send confirmation message and then delete it after
	msg := &discordgo.Message{}
	if len(usernames) != 0 {
		msg, err = s.ChannelMessageSend(m.ChannelID, "Removed the latest "+strconv.Itoa(num-1)+" messages from the following people: "+userText)
	} else {
		msg, err = s.ChannelMessageSend(m.ChannelID, "Removed the latest "+strconv.Itoa(num-1)+" messages.")
	}
	if err != nil {
		return
	}
	timer := time.NewTimer(5 * time.Second)
	<-timer.C
	s.ChannelMessageDelete(msg.ChannelID, msg.ID)
}
