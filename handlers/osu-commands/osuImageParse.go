package osucommands

import (
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"

	osuapi "../../osu-api"
	osutools "../../osu-functions"
	structs "../../structs"
	tools "../../tools"
	"github.com/bwmarrin/discordgo"
)

// OsuImageParse detects for an osu image
func OsuImageParse(s *discordgo.Session, m *discordgo.MessageCreate, linkRegex *regexp.Regexp, osuAPI *osuapi.Client, cache []structs.MapData) {

	// Create regexps for checks
	mapperRegex, _ := regexp.Compile(`(?i)b?e?a?t?mapp?e?d? by (\S*)`)
	titleRegex, _ := regexp.Compile(`\- (.*) \[`)
	diffRegex, _ := regexp.Compile(`\[(.*)\]`)
	diagnosisRegex, _ := regexp.Compile(` -v`)

	url := linkRegex.FindStringSubmatch(m.Content)[0]
	if len(m.Attachments) > 0 {
		url = m.Attachments[0].URL
	} else if len(m.Embeds) > 0 && m.Embeds[0].Image != nil {
		url = m.Embeds[0].Image.URL
	}

	// Fetch the image data
	response, err := http.Get(url)
	if err != nil {
		return
	}
	imgSrc, _, err := image.Decode(response.Body)
	if err != nil {
		return
	}

	// Convert image to grayscale
	newImg := imaging.AdjustSaturation(imgSrc, -100)
	b := newImg.Bounds()
	newImg = imaging.Crop(imgSrc, image.Rect(0, 0, b.Dx(), int(math.Max(120.0*float64(b.Dy())/969.0, 120.0))))

	// Check if name already exists, create a new name via integer suffix instead if target name is currently in use
	name := strconv.Itoa(rand.Intn(10000000))
	_, err1 := os.Stat("./" + name + ".png")
	_, err2 := os.Stat("./" + name + ".txt")
	if !os.IsNotExist(err1) || !os.IsNotExist(err2) {
		i := 1
		for {
			_, err1 := os.Stat("./" + name + strconv.Itoa(i) + ".png")
			_, err2 := os.Stat("./" + name + strconv.Itoa(i) + ".txt")
			if os.IsNotExist(err1) && os.IsNotExist(err2) {
				name = name + strconv.Itoa(i)
				break
			} else {
				i++
			}
		}
	}

	// Create the file to write in
	file, err := os.Create("./" + name + ".png")
	tools.ErrRead(err)

	// Dump the image data into the file
	png.Encode(file, newImg)
	tools.ErrRead(err)

	// Close file and res
	response.Body.Close()
	file.Close()

	// Run tesseract to parse the image
	_, err = exec.Command("tesseract", "./"+name+".png", name, "--dpi", "96").Output()
	tools.ErrRead(err)

	// Read result and parse it
	text, err := ioutil.ReadFile(name + ".txt")
	tools.ErrRead(err)

	// Parse result
	raw := string(text)
	str := strings.Split(raw, "\n")
	if len(str) < 2 {
		tools.DeleteFile("./" + name + ".png")
		tools.DeleteFile("./" + name + ".txt")
		return
	}

	var (
		title      string
		mapperName string
		diff       string
	)
	for _, line := range str {
		if mapperRegex.MatchString(line) {
			mapperName = mapperRegex.FindStringSubmatch(line)[1]
		} else if titleRegex.MatchString(line) {
			title = titleRegex.FindStringSubmatch(line)[1]
			if diffRegex.MatchString(line) {
				diff = diffRegex.FindStringSubmatch(line)[1]
			}
		} else if diffRegex.MatchString(line) {
			diff = diffRegex.FindStringSubmatch(line)[1]
		}
	}

	if mapperName == "" || title == "" {
		tools.DeleteFile("./" + name + ".png")
		tools.DeleteFile("./" + name + ".txt")
		return
	}

	message, _ := s.ChannelMessageSend(m.ChannelID, "Processing image...")
	var beatmap osuapi.Beatmap
	beatmaps, err := osuAPI.GetBeatmaps(osuapi.GetBeatmapsOpts{
		Username: mapperName,
	})
	if err != nil {
		if diagnosisRegex.MatchString(m.Message.Content) {
			s.ChannelMessageEdit(message.ChannelID, message.ID, "No luck... the mapper line I parsed was ** "+mapperName+" ** and the title line I parsed was ** "+title+" **")
		} else {
			s.ChannelMessageDelete(message.ChannelID, message.ID)
		}
		tools.DeleteFile("./" + name + ".png")
		tools.DeleteFile("./" + name + ".txt")
		return
	}

	// Reorder the maps so that it returns the highest difficulty in the set
	sort.Slice(beatmaps, func(i, j int) bool {
		return beatmaps[i].DifficultyRating > beatmaps[j].DifficultyRating
	})

	// Look for the beatmap in the results
	warning := ""
	for _, b := range beatmaps {
		if b.Title == title {
			if diff != "" {
				if b.DiffName == diff {
					beatmap = b
					break
				}
			} else {
				beatmap = b
				break
			}
		}
	}

	// Run it again in case no map with the diff name was found due to possible image parsing errors
	if beatmap == (osuapi.Beatmap{}) {
		warning = "**WARNING** Diff name could not be found. Showing information for top diff."
		for _, b := range beatmaps {
			if b.Title == title {
				beatmap = b
				break
			}
		}
	}

	// Check if anything was actually found
	if beatmap == (osuapi.Beatmap{}) || len(beatmaps) == 0 {
		if diagnosisRegex.MatchString(m.Message.Content) {
			s.ChannelMessageEdit(message.ChannelID, message.ID, "No luck... the mapper line I parsed was ** "+mapperName+" ** and the title line I parsed was ** "+title+" **")
		} else {
			s.ChannelMessageDelete(message.ChannelID, message.ID)
		}
		tools.DeleteFile("./" + name + ".png")
		tools.DeleteFile("./" + name + ".txt")
		return
	}

	// Download the .osu file for the map
	replacer, _ := regexp.Compile(`[^a-zA-Z0-9\s\(\)]`)
	tools.DownloadFile(
		"./data/osuFiles/"+
			strconv.Itoa(beatmap.BeatmapID)+
			" "+
			replacer.ReplaceAllString(beatmap.Artist, "")+
			" - "+
			replacer.ReplaceAllString(beatmap.Title, "")+
			".osu",
		"https://osu.ppy.sh/osu/"+
			strconv.Itoa(beatmap.BeatmapID))
	// Assign embed colour for different modes
	Color := osutools.ModeColour(beatmap.Mode)

	// Obtain whole set
	beatmaps, err = osuAPI.GetBeatmaps(osuapi.GetBeatmapsOpts{
		BeatmapSetID: beatmap.BeatmapSetID,
	})
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "The osu! API just owned me. Please try again!")
		return
	}

	// Assign variables for map specs
	totalMinutes := math.Floor(float64(beatmap.TotalLength / 60))
	totalSeconds := fmt.Sprint(math.Mod(float64(beatmap.TotalLength), float64(60)))
	if len(totalSeconds) == 1 {
		totalSeconds = "0" + totalSeconds
	}
	hitMinutes := math.Floor(float64(beatmap.HitLength / 60))
	hitSeconds := fmt.Sprint(math.Mod(float64(beatmap.HitLength), float64(60)))
	if len(hitSeconds) == 1 {
		hitSeconds = "0" + hitSeconds
	}

	length := "**Length:** " + fmt.Sprint(totalMinutes) + ":" + fmt.Sprint(totalSeconds) + " (" + fmt.Sprint(hitMinutes) + ":" + fmt.Sprint(hitSeconds) + ") "
	bpm := "**BPM:** " + fmt.Sprint(beatmap.BPM) + " "
	combo := "**FC:** " + strconv.Itoa(beatmap.MaxCombo) + "x"
	mapStats := "**CS:** " + strconv.FormatFloat(beatmap.CircleSize, 'f', 1, 64) + " **AR:** " + strconv.FormatFloat(beatmap.ApproachRate, 'f', 1, 64) + " **OD:** " + strconv.FormatFloat(beatmap.OverallDifficulty, 'f', 1, 64) + " **HP:** " + strconv.FormatFloat(beatmap.HPDrain, 'f', 1, 64)
	mapObjs := "**Circles:** " + strconv.Itoa(beatmap.Circles) + " **Sliders:** " + strconv.Itoa(beatmap.Sliders) + " **Spinners:** " + strconv.Itoa(beatmap.Spinners)

	status := "**Rank Status:** " + beatmap.Approved.String()

	download := "**Download:** [osz link](https://osu.ppy.sh/d/" + strconv.Itoa(beatmap.BeatmapSetID) + ")" + " | <osu://dl/" + strconv.Itoa(beatmap.BeatmapSetID) + ">"
	var diffs string
	if len(beatmaps) == 1 {
		diffs = "**1** difficulty <:ahFuck:550808614202245131>"
	} else {
		diffs = "**" + strconv.Itoa(len(beatmaps)) + "** difficulties <:ahFuck:550808614202245131>"
	}

	// Calculate SR and PP
	starRating, ppSS, pp99, pp98, pp97, pp95 := osutools.BeatmapCache("NM", beatmap, cache)

	// Create embed
	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			URL:     "https://osu.ppy.sh/beatmaps/" + strconv.Itoa(beatmap.BeatmapID),
			Name:    beatmap.Artist + " - " + beatmap.Title + " by " + beatmap.Creator,
			IconURL: "https://a.ppy.sh/" + strconv.Itoa(beatmap.CreatorID) + "?" + strconv.Itoa(rand.Int()) + ".jpeg",
		},
		Color: Color,
		Description: starRating + length + bpm + combo + "\n" +
			mapStats + "\n" +
			mapObjs + "\n" +
			status + "\n" +
			download + "\n" +
			diffs + "\n" + "\n" +
			"**[" + beatmap.DiffName + "]** " + warning + "\n" +
			//aimRating + speedRating + totalRating + "\n" + TODO: Make SR calc work
			ppSS + pp99 + pp98 + pp97 + pp95,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://b.ppy.sh/thumb/" + strconv.Itoa(beatmap.BeatmapSetID) + "l.jpg",
		},
	}
	if strings.ToLower(beatmap.Title) == "crab rave" {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: "https://cdn.discordapp.com/emojis/510169818893385729.gif",
		}
	}
	s.ChannelMessageEdit(message.ChannelID, message.ID, "")
	time.Sleep(time.Millisecond)
	s.ChannelMessageEditEmbed(message.ChannelID, message.ID, embed)

	// Close files
	tools.DeleteFile("./" + name + ".png")
	tools.DeleteFile("./" + name + ".txt")
	return
}
