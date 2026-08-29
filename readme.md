# `ftl` - fuck the limit
 *basic command-line tool to simplify encoding vp9 videos within discord's 10MB file size limit*

## prerequisites
 have `ffmpeg` & `ffprobe` downloaded and available to your environmental system paths<br><br>
 you can install it with the follow commands: <br>
 windows: `winget install Gyan.FFmpeg`<br>
 debian: `apt install ffmpeg`

## building
have [golang](https://go.dev/doc/install) installed and run the following in terminal<br>
```
git clone https://github.com/dtbead/ftl
cd ftl
go build
```

## usage
```
  -60
        limit the fps to 60. implicitly implied by the -smooth flag
  -debug
        print debug information during execution
  -downscale
        downscale resolution if greater than 720p
  -fast
        increase speed of encoding at cost of potential quality loss
  -path string
        filepath to video
  -seek string
        skip mm:ss OR ss into the video before encoding
  -size int
        desired filesize (in MBs) to constrain video into (default 10)
  -smooth
        blend the framerate of 120 fps into 60
```
