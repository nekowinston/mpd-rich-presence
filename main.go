package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/log"
	"github.com/cheshir/ttlcache"
	"github.com/fhs/gompd/v2/mpd"
	"github.com/hugolgst/rich-go/client"
	"github.com/shkh/lastfm-go/lastfm"
	v "github.com/spf13/viper"
)

const (
	apiKey       = "2236babefa8ebb3d93ea467560d00d04"
	apiSecret    = "94d9a09c0cd5be955c4afaeaffcaefcd"
	statePlaying = "play"
)

var (
	artworkCache = ttlcache.New(time.Minute)
	songCache    = ttlcache.New(time.Minute)
	urlCache     = ttlcache.New(time.Minute)
	mpdClient    *mpd.Client
	lastfmAPI    *lastfm.Api
	err          error
	c            *Config
)

func init() {
	v.SetConfigName("mpd-rich-presence")
	v.SetConfigType("yaml")
	v.AddConfigPath("$XDG_CONFIG_HOME")
	v.AddConfigPath("$HOME/.config")
	v.AddConfigPath(".")

	v.SetDefault("branding", "mpd")
	v.SetDefault("use_socket", false)
	v.SetDefault("host", "127.0.0.1")
	v.SetDefault("port", 6600)

	v.SetDefault("sleep.long", 30*time.Second)
	v.SetDefault("sleep.short", 5*time.Second)

	v.SetDefault("rich_presence.image.large", "%album%")
	v.SetDefault("rich_presence.image.small", "%title%")
	v.SetDefault("rich_presence.upper", "%title%")
	v.SetDefault("rich_presence.lower", "by %artist% (%album%)")
	v.SetDefault("rich_presence.button", "View on Last.fm")
	v.SetDefault("rich_presence.time", "elapsed")

	v.SetDefault("lastfm.enabled", true)
	v.SetDefault("lastfm.apikey", apiKey)
	v.SetDefault("lastfm.apisecret", apiSecret)

	if err = v.ReadInConfig(); err != nil {
		log.Warn("No config file found, using defaults.")
	}

	if err = v.Unmarshal(&c); err != nil {
		log.WithError(err).Fatal("failed to unmarshal config")
	}
}

func main() {
	defer func() {
		_ = songCache.Close()
		_ = artworkCache.Close()
		_ = urlCache.Close()
	}()

	if c.LastFM.Enabled {
		lastfmAPI = lastfm.New(c.LastFM.APIKey, c.LastFM.APISecret)
	}

	if c.UseSocket {
		mpdClient, err = mpd.Dial("unix", fmt.Sprintf("%s", c.Host))
	} else {
		mpdClient, err = mpd.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port))
	}
	if err != nil {
		log.WithError(err).Fatal("failed to connect to MPD server")
	}
	defer mpdClient.Close()

	if os.Getenv("DEBUG") != "" {
		log.SetLevelFromString("debug")
	}
	ac := activityConnection{}
	defer func() { ac.stop() }()

	for {
		details, err := getNowPlaying()
		if err != nil {
			log.WithError(err).
				WithField("sleep", c.Sleep.Short).
				Warn("will try again soon")
			ac.stop()
			time.Sleep(c.Sleep.Short)
			continue
		}

		if details.State != statePlaying {
			if ac.connected {
				log.Info("not playing")
				ac.stop()
			}
			time.Sleep(c.Sleep.Short)
			continue
		}

		if err := ac.play(details); err != nil {
			log.WithError(err).
				Warn("could not set activity, will retry later")
		}

		time.Sleep(c.Sleep.Short)
	}
}

func getNowPlaying() (Details, error) {
	init := time.Now()
	defer func() {
		log.WithField("took", time.Since(init)).Debug("got info")
	}()

	status, err := mpdClient.Status()
	if err != nil {
		return Details{}, err
	}
	state := status["state"]
	if state != statePlaying {
		return Details{
			State: state,
		}, nil
	}

	current, err := mpdClient.CurrentSong()
	if err != nil {
		return Details{}, err
	}

	songID, err := strconv.ParseInt(current["Id"], 10, 64)
	if err != nil {
		return Details{}, err
	}

	timeSplit := strings.Split(status["time"], ":")

	position, err := time.ParseDuration(timeSplit[0] + "s")
	if err != nil {
		return Details{}, err
	}

	cached, ok := songCache.Get(ttlcache.Int64Key(songID))
	if ok {
		log.WithField("songID", songID).Debug("got song from cache")
		return Details{
			Song:     cached.(Song),
			Position: position,
			State:    state,
		}, nil
	}

	name := current["Title"]
	artist := current["Artist"]
	album := current["Album"]
	albumArtist := current["AlbumArtist"]
	genre := current["Genre"]

	year, _ := strconv.Atoi(strings.Split(current["Date"], "-")[0])
	duration, err := time.ParseDuration(timeSplit[1] + "s")
	if err != nil {
		return Details{}, err
	}

	var shareURL, artworkURL string
	if c.LastFM.Enabled {
		shareURL, artworkURL, err = getArtwork(firstNonEmpty(albumArtist, artist), album)
		if err != nil {
			shareURL, artworkURL = "", ""
			log.WithError(err).Warn("could not get album artwork")
		}
	}

	song := Song{
		ID:          songID,
		Name:        name,
		Artist:      artist,
		Album:       album,
		AlbumArtist: albumArtist,
		Genre:       genre,
		Year:        year,
		Duration:    duration,
		Artwork:     artworkURL,
		ShareURL:    shareURL,
	}

	songCache.Set(ttlcache.Int64Key(songID), song, 24*time.Hour)

	return Details{
		Song:     song,
		Position: position,
		State:    state,
	}, nil
}

func fmtActivity(s string, d Details) string {
	song := d.Song
	s = strings.ReplaceAll(s, "%album%", song.Album)
	s = strings.ReplaceAll(s, "%artist%", song.Artist)
	s = strings.ReplaceAll(s, "%title%", song.Name)
	s = strings.ReplaceAll(s, "%year%", strconv.Itoa(song.Year))
	s = strings.ReplaceAll(s, "%genre%", song.Genre)
	return s
}

type Details struct {
	Song     Song
	Position time.Duration
	State    string
}

type Song struct {
	ID            int64
	Name          string
	Artist        string
	Album         string
	AlbumArtist   string
	Genre         string
	Year          int
	Duration      time.Duration
	Artwork       string
	ArtworkSource string
	ShareURL      string
}

func getArtwork(artist, album string) (string, string, error) {
	key := url.QueryEscape(strings.Join([]string{artist, album}, " "))
	cachedURL, urlOk := urlCache.Get(ttlcache.StringKey(key))
	cachedArtwork, artOk := artworkCache.Get(ttlcache.StringKey(key))
	if artOk && urlOk {
		log.WithField("key", key).Debug("got album artwork from cache")
		return cachedURL.(string), cachedArtwork.(string), nil
	}

	res, err := lastfmAPI.Album.GetInfo(lastfm.P{
		"artist": artist,
		"album":  album,
	})
	if err != nil {
		return "", "", err
	}
	shareURL := ""
	artwork := ""
	if res.Images[2].Url != "" {
		artwork = res.Images[2].Url
	}
	if res.Url != "" {
		shareURL = res.Url
	}
	artworkCache.Set(ttlcache.StringKey(key), artwork, time.Hour)
	urlCache.Set(ttlcache.StringKey(key), shareURL, time.Hour)
	return shareURL, artwork, nil
}

type activityConnection struct {
	connected    bool
	lastSongID   int64
	lastPosition time.Duration
}

func (ac *activityConnection) stop() {
	if ac.connected {
		client.Logout()
		ac.connected = false
		ac.lastPosition = 0.0
		ac.lastSongID = 0
	}
}

func (ac *activityConnection) play(details Details) error {
	song := details.Song
	if ac.lastSongID == song.ID {
		after := details.Position >= ac.lastPosition
		within := details.Position-ac.lastPosition < c.Sleep.Short+time.Second
		if after && within {
			progress := details.Position.Seconds() / song.Duration.Seconds()
			log.
				WithField("songID", song.ID).
				WithField("position", details.Position).
				WithField("progress", fmt.Sprintf("%.2f", progress*100)).
				Debug("ongoing activity, ignoring")
			ac.lastPosition = details.Position
			return nil
		}
	}
	log.
		WithField("lastSongID", ac.lastSongID).
		WithField("songID", song.ID).
		WithField("lastPosition", ac.lastPosition).
		WithField("position", details.Position).
		Debug("new event")

	ac.lastPosition = details.Position
	ac.lastSongID = song.ID

	start := time.Now().Add(-1 * details.Position)
	end := time.Now().Add(song.Duration - details.Position)
	if !ac.connected {
		if err := client.Login("1037215044141854721"); err != nil {
			log.WithError(err).Fatal("could not create rich presence client")
		}
		ac.connected = true
	}

	var buttons []*client.Button
	if song.ShareURL != "" {
		buttons = []*client.Button{
			{
				Label: fmtActivity(c.RP.Button, details),
				Url:   song.ShareURL,
			},
		}
	}

	timeStamps := client.Timestamps{}
	timeStamps.Start = &start

	if c.RP.Time == "remaining" {
		timeStamps.End = &end
	}

	largeImage := "logo"
	smallImage := "nowplaying"
	if c.Branding == "lastfm" {
		largeImage = "lastfm"
		smallImage = "lastfm-nowplaying"
	}

	if err := client.SetActivity(client.Activity{
		Details:    fmtActivity(c.RP.Upper, details),
		State:      fmtActivity(c.RP.Lower, details),
		LargeText:  fmtActivity(c.RP.Image.Large, details),
		SmallText:  fmtActivity(c.RP.Image.Small, details),
		LargeImage: firstNonEmpty(song.Artwork, largeImage),
		SmallImage: smallImage,
		Timestamps: &timeStamps,
		Buttons:    buttons,
	}); err != nil {
		return err
	}

	log.WithField("song", song.Name).
		WithField("album", song.Album).
		WithField("artist", song.Artist).
		WithField("year", song.Year).
		WithField("duration", song.Duration).
		WithField("position", details.Position).
		Info("now playing")
	return nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

type Config struct {
	Branding  string
	UseSocket bool `mapstructure:"use_socket"`
	Host      string
	Port      uint16

	Sleep struct {
		Long  time.Duration
		Short time.Duration
	}
	RP struct {
		Image struct {
			Large string
			Small string
		}
		Upper  string
		Lower  string
		Button string
		Time   string
	} `mapstructure:"rich_presence"`
	LastFM struct {
		Enabled   bool
		APIKey    string
		APISecret string
	} `mapstructure:"lastfm"`
}
