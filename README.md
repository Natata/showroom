# Showroom Live Stream Recorder

Automatically records live streams from Showroom (https://www.showroom-live.com/) to TS files.

## Features

- Monitors a Showroom room for live status every 3 seconds
- Automatically starts recording when the room goes live
- Downloads the highest quality stream available
- Saves each live session to a separate TS file (native HLS format)
- Continuously monitors for multiple live sessions

## Prerequisites

- Go 1.21 or higher
- ffmpeg installed and available in PATH

### Installing ffmpeg on macOS

```bash
brew install ffmpeg
```

## Usage

### Basic Usage

```bash
go run main.go <room_url>
```

### Examples

```bash
# Monitor and record a specific room
go run main.go https://www.showroom-live.com/r/watashi_idol_0196

# Another example
go run main.go https://www.showroom-live.com/r/ss00013
```

### Building the Binary

```bash
go build -o showroom-recorder
./showroom-recorder https://www.showroom-live.com/r/watashi_idol_0196
```

## How It Works

1. Parses the room URL to extract the room identifier (room_url_key)
2. Polls the Showroom API every 3 seconds to check if the room is live
3. When the room goes live:
   - Fetches available streaming URLs
   - Selects the highest quality stream
   - Starts ffmpeg to download and save the stream as TS
4. When the room goes offline:
   - Stops the current recording
   - Continues monitoring for the next live session
5. Each live session is saved as a separate file

## Output Files

Recordings are saved in the `recordings/` directory with the following naming format:

```
{room_id}_{timestamp}_{count}.ts
```

Example:
```
recordings/513601_20250102_143022_1.ts
recordings/513601_20250102_190515_2.ts
```

## Stopping the Recorder

Press `Ctrl+C` to gracefully stop the recorder. Any ongoing recording will be properly finalized.

## Multiple Rooms

To monitor multiple rooms simultaneously, run the program in separate terminal windows:

```bash
# Terminal 1
go run main.go https://www.showroom-live.com/r/room1

# Terminal 2
go run main.go https://www.showroom-live.com/r/room2
```

## Troubleshooting

### "ffmpeg: command not found"
Make sure ffmpeg is installed and available in your PATH.

### No recordings created
- Check if the room URL is correct
- Verify the room is actually live by visiting the URL in a browser
- Check the console output for error messages

### Recording stops unexpectedly
- Check your network connection
- Review ffmpeg error messages in the console
