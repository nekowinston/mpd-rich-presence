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
)

const (
	apiKey       = "2236babefa8ebb3d93ea467560d00d04"
	apiSecret    = "94d9a09c0cd5be955c4afaeaffcaefcd"
	longSleep    = 30 * time.Second
	shortSleep   = 5 * time.Second
	statePlaying = "play"
)

var (
	artworkCache = ttlcache.New(time.Minute)
	songCache    = ttlcache.New(time.Minute)
	urlCache     = ttlcache.New(time.Minute)
	mpdClient    *mpd.Client
	lastfmAPI    = lastfm.New(apiKey, apiSecret)
	err          error
)

func main() {
	defer func() {
		_ = songCache.Close()
		_ = artworkCache.Close()
		_ = urlCache.Close()
	}()

	// Connect to MPD server
	mpdClient, err = mpd.Dial("tcp", "127.0.0.1:6600")
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
			log.WithError(err).WithField("sleep", shortSleep).Warn("will try again soon")
			ac.stop()
			time.Sleep(shortSleep)
			continue
		}

		if details.State != statePlaying {
			if ac.connected {
				log.Info("not playing")
				ac.stop()
			}
			time.Sleep(shortSleep)
			continue
		}

		if err := ac.play(details); err != nil {
			log.WithError(err).Warn("could not set activity, will retry later")
		}

		time.Sleep(shortSleep)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
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

	initialState, err := mpdClient.CurrentSong()
	if err != nil {
		return Details{}, err
	}

	songID, err := strconv.ParseInt(initialState["Id"], 10, 64)
	if err != nil {
		return Details{}, err
	}

	position, err := strconv.ParseFloat(strings.Split(status["time"], ":")[0], 64)
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

	name := string(initialState["Title"])
	artist := string(initialState["Artist"])
	album := string(initialState["Album"])
	year, _ := strconv.Atoi(strings.Split(string(initialState["Date"]), "-")[0])
	duration, err := strconv.ParseFloat(initialState["Time"], 64)
	if err != nil {
		return Details{}, err
	}

	url, artworkURL, err := getArtwork(artist, album)
	if err != nil {
		url, artworkURL = "", ""
		log.WithError(err).Warn("could not get album artwork")
	}

	song := Song{
		ID:       songID,
		Name:     name,
		Artist:   artist,
		Album:    album,
		Year:     year,
		Duration: duration,
		Artwork:  artworkURL,
		URL:      url,
	}

	songCache.Set(ttlcache.Int64Key(songID), song, 24*time.Hour)

	return Details{
		Song:     song,
		Position: position,
		State:    state,
	}, nil
}

type Details struct {
	Song     Song
	Position float64
	State    string
}

type Song struct {
	ID            int64
	Name          string
	Artist        string
	Album         string
	Year          int
	Duration      float64
	Artwork       string
	ArtworkSource string
	URL           string
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
	url := ""
	artwork := ""
	if res.Images[2].Url != "" {
		artwork = res.Images[2].Url
	}
	if res.Url != "" {
		url = res.Url
	}
	artworkCache.Set(ttlcache.StringKey(key), artwork, time.Hour)
	urlCache.Set(ttlcache.StringKey(key), url, time.Hour)
	return url, artwork, nil
}

type activityConnection struct {
	connected    bool
	lastSongID   int64
	lastPosition float64
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
		if details.Position >= ac.lastPosition {
			log.
				WithField("songID", song.ID).
				WithField("position", details.Position).
				WithField("progress", details.Position/song.Duration).
				Debug("ongoing activity, ignoring")
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

	start := time.Now().Add(-1 * time.Duration(details.Position) * time.Second)
	end := time.Now().Add(time.Duration(song.Duration-details.Position) * time.Second)
	if !ac.connected {
		if err := client.Login("1037215044141854721"); err != nil {
			log.WithError(err).Fatal("could not create rich presence client")
		}
		ac.connected = true
	}

	var buttons []*client.Button
	if song.URL != "" {
		buttons = []*client.Button{
			{
				Label: "View on Last.fm",
				Url:   song.URL,
			},
		}
	}

	if err := client.SetActivity(client.Activity{
		State:      fmt.Sprintf("by %s", song.Artist),
		Details:    song.Name,
		LargeImage: firstNonEmpty(song.Artwork, "logo"),
		SmallImage: "nowplaying",
		LargeText:  song.Album,
		SmallText:  fmt.Sprintf("%s by %s", song.Name, song.Artist),
		Timestamps: &client.Timestamps{
			Start: timePtr(start),
			End:   timePtr(end),
		},
		Buttons: buttons,
	}); err != nil {
		return err
	}

	log.WithField("song", song.Name).
		WithField("album", song.Album).
		WithField("artist", song.Artist).
		WithField("year", song.Year).
		WithField("duration", time.Duration(song.Duration)*time.Second).
		WithField("position", time.Duration(details.Position)*time.Second).
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
