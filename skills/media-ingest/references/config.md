# Configuration

Optional. Without a config file every option takes its default, and options set to `"ask"` are asked once per run.

**Location:** `~/.config/media-ingest/config.json` on Windows, macOS, and Linux alike (on Windows, `~` is `C:\Users\<you>`). `$XDG_CONFIG_HOME/media-ingest/config.json` is used when that variable is set, and the `MEDIA_INGEST_CONFIG` environment variable can point anywhere.

```bash
"<mi>" config init   # write a starter file with every default
"<mi>" config show   # print the effective settings
"<mi>" config path   # print where the file is
```

The config file lives outside the skill folder, so personal paths never end up in the skill (or its git repo).

## Options

```json
{
  "source": null,
  "destinations": { "movies": null, "tv": null, "music": null },
  "video": { "episode_titles": "ask" },
  "music": { "metadata": "ask", "lyrics": "ask", "plain_lyrics": false },
  "contact": null,
  "after_ingest": []
}
```

| Key | Default | Meaning |
|---|---|---|
| `source` | `null` | Folder to process when the user doesn't name one. |
| `destinations.movies` / `.tv` / `.music` | `null` | Library root for each type. `null` organizes in place inside the source folder. Paths can use `~` and environment variables. A NAS share works if it's mounted (SMB/NFS); cross-drive moves are checksum-verified. |
| `video.episode_titles` | `"ask"` | `true`: look up TV episode titles on TVmaze. `false`: name episodes `SXXEXX.ext`. |
| `music.metadata` | `"ask"` | `true`: fix tags against MusicBrainz, embed official cover art, strip junk. `false`: rename using existing tags only. |
| `music.lyrics` | `"ask"` | `true`: fetch lyrics from LRCLIB after organizing. `false`: skip lyrics. |
| `music.plain_lyrics` | `false` | Also save unsynced lyrics as `.txt` when no synced version exists. Off by default, because some players keep showing plain lyrics even after a synced `.lrc` arrives. |
| `contact` | `null` | An email or URL added to the User-Agent sent to MusicBrainz, which [asks for contact info](https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting). Optional. |
| `after_ingest` | `[]` | Plain-language follow-up steps Claude carries out after an ingest, e.g. `"For music: run my upload-to-server skill on the new albums"`. Use this to chain your own private skills (uploads, playlists, server rescans) without putting personal details in this one. |

Any option left as `"ask"` is asked once per run, together with the others, and only when that media type is present.
