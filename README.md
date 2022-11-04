# MPD Discord Rich Presence

Gets your current track from MPD, and shows it in your Discord Rich Presence.

It also gets metadata for the song via Last.fm's API, such as the album cover.
People clicking on your profile can easily view your current track on last.fm.

<p align="center">
  <img src="https://user-images.githubusercontent.com/79978224/199865008-746183c4-f6d5-4e4c-94a0-9f32cfb96eaa.png"/>
</p>

### Installation

#### Linux

Download the binary from the [latest release][release], and execute it.

#### macOS

A Homebrew tap is available:

```bash
nekowinston/tap/mpd-rich-presence
```

Then start & enable the service:

```
brew services start nekowinston/tap/mpd-rich-presence
```

Remember to restart this service after updates.

### Credits

Forked from [@caarlos0][caarlos0]'s repository:
[Rich Presence from Apple Music][applemusic]

[mpd]: https://github.com/MusicPlayerDaemon/MPD
[release]: https://github.com/nekowinston/mpd-rich-presence/releases/latest
[caarlos0]: https://github.com/caarlos0
[applemusic]: https://github.com/caarlos0/discord-applemusic-rich-presence
