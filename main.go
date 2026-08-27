package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ArgBuilder []string

func (a *ArgBuilder) Add(key, value string) {
	if key != "" {
		*a = append(*a, key)
	}

	if value != "" {
		*a = append(*a, value)
	}
}

func NewArgsBuilder() ArgBuilder {
	return make(ArgBuilder, 0, 10) // pre-allocate 10 key-value arguments
}

const FILTER_DOWNSCALE = `scale='if(gt(iw,1280),1280,-1)':'if(gt(ih,720),720,-1)':force_original_aspect_ratio=decrease`
const AUDIO_BITRATE = 48 // kilobits

var (
	OPT_PATH      = ""
	OPT_SEEK      = ""
	OPT_SIZE      = 10
	OPT_SMOOTH    = false
	OPT_FAST      = false
	OPT_DOWNSCALE = false
	OPT_DEBUG     = false
)

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func exitErr(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}

// MeasureBitrate calculates the needed bitrate (in kilobits) to achieve
// a specific filesize in MBs according to a given duration (in seconds).
func MeasureBitrate(duration float64, filesize int) float64 {
	return (float64(filesize) / duration) * 8000
}

func GetDuration(ctx context.Context, filepath string) (seconds float64, err error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-i", filepath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1")

	out, err := cmd.Output()
	if err != nil {
		return -1, err
	}

	seconds, err = strconv.ParseFloat(strings.TrimSuffix(string(out), "\r\n"), 64)
	if err != nil {
		return -1, errors.New("failed to parse duration")
	}

	if seconds < 1 {
		return -1, fmt.Errorf("got invalid duration of %d", seconds)
	}

	return seconds, nil
}

func main() {
	// init cmd flags
	flag.StringVar(&OPT_PATH, "path", "", "filepath to video")
	flag.BoolVar(&OPT_SMOOTH, "smooth", false, "blend the framerate of 120 fps into 60. can impact encoding speed")
	flag.BoolVar(&OPT_FAST, "fast", false, "increase speed of encoding at cost of potential quality loss")
	flag.BoolVar(&OPT_DOWNSCALE, "downscale", false, "downscale resolution if greater than 720p")
	flag.BoolVar(&OPT_DEBUG, "debug", false, "print debug information during execution")
	flag.StringVar(&OPT_SEEK, "seek", "", "skip mm:ss OR ss into the video before encoding")
	flag.IntVar(&OPT_SIZE, "size", 10, "desired filesize (in MBs) to constrain video into")
	flag.Parse()

	if !isFlagPassed("path") {
		exitErr("no path given")
	}

	if _, err := os.Stat(OPT_PATH); errors.Is(err, os.ErrNotExist) {
		exitErr("filepath does not exist")
	}

	// build context in case we need to exit later
	ctx := context.Background()

	// init arguments that will always be needed
	argsBase := ArgBuilder{
		"-i", OPT_PATH,
		"-c:v", "libvpx-vp9",
		"-crf", "20",
		"-bf", "2",
		"-lag-in-frames", "25",
		"-enable-tpl", "1",
		"-pix_fmt", "yuv420p10le",
		"-profile:v", "2",
	}

	argsFirstPass, argsSecondPass := NewArgsBuilder(), NewArgsBuilder()
	argsFilter := strings.Builder{}

	if OPT_FAST {
		argsFirstPass.Add("-pass", "1")
		argsSecondPass.Add("-pass", "2")
		argsSecondPass.Add("-deadline", "realtime")
		argsSecondPass.Add("-cpu-used", "4")
	} else {
		argsFirstPass.Add("-pass", "1")
		argsSecondPass.Add("-pass", "2")
		argsSecondPass.Add("-cpu-used", "2")
	}

	duration, err := GetDuration(ctx, OPT_PATH)
	if err != nil {
		exitErr("failed to get duration of video, %v", err)
	}

	if isFlagPassed("seek") {
		if !strings.Contains(OPT_SEEK, ":") {
			seconds, err := strconv.Atoi(OPT_SEEK)
			if err != nil {
				exitErr("failed to parse seek value, %v", err)
			}

			duration -= float64(seconds)

			argsBase.Add("-ss", "00:00:"+OPT_SEEK)
		} else {
			var mins, secs int
			if _, err := fmt.Sscan(OPT_SEEK, "00:%d:%d", &mins, &secs); err != nil {
				exitErr("failed to parse seek value. %v", err)
			}
			duration -= float64(mins*60 + secs)

			argsBase.Add("-ss", "00:"+OPT_SEEK)
		}
	}

	avgBitrate := MeasureBitrate(duration, OPT_SIZE-(AUDIO_BITRATE/1024))

	// discord allows uploading 15 MB videos, but we'll try not to hit that limit
	// unless we really need to for quality sake
	if OPT_SIZE < 15 && OPT_SIZE > 6 {
		MaxBitrate := MeasureBitrate(duration, OPT_SIZE+5-(AUDIO_BITRATE/1024))
		minBitrate := MeasureBitrate(duration, OPT_SIZE-5-(AUDIO_BITRATE/1024))

		argsBase.Add("-minrate", strconv.FormatFloat(minBitrate, 'f', 2, 32)+"k")
		argsBase.Add("-maxrate", strconv.FormatFloat(MaxBitrate, 'f', 2, 32)+"k")
	}
	argsBase.Add("-b:v", strconv.FormatFloat(avgBitrate, 'f', 2, 32)+"k") // avg bitrate

	// append x-pass arguments. we do it after determining speed args
	// from above to keep ffmpeg happy.
	argsFirstPass.Add("-an", "")
	argsFirstPass.Add("-f", "null")
	argsSecondPass.Add("-c:a", "libopus")
	argsSecondPass.Add("-filter:a", "loudnorm")
	argsSecondPass.Add("-b:a", strconv.Itoa(AUDIO_BITRATE)+"k")
	argsSecondPass.Add("-y", "")

	filepathOutput := strings.TrimSuffix(filepath.Base(OPT_PATH), filepath.Ext(filepath.Base(OPT_PATH))) + "-vp9.webm"
	argsSecondPass.Add(filepathOutput, "")

	if OPT_DOWNSCALE {
		argsFilter.WriteString(FILTER_DOWNSCALE)
	} else {
		// add multi-threading speed optimization if we're not going to downscale
		argsBase.Add("-tile-rows", "1")
		argsBase.Add("-row-mt", "1")
	}

	if OPT_SMOOTH {
		if OPT_DOWNSCALE {
			// ffmpeg expects a filter output (first [v]) and input (second [v])
			// when being combined with multiple filters
			argsFilter.WriteString("[v];[v]tblend=all_mode=average")
		} else {
			argsFilter.WriteString("[v]tblend=all_mode=average") // singular [v] input for blend
		}
		argsBase.Add("-r", "60") // set framerate to 60 fps
	}

	if argsFilter.Len() > 0 {
		argsBase.Add("-filter_complex", argsFilter.String())
	}

	if OPT_DEBUG {
		fmt.Printf("first pass:\n\t'ffmpeg \"%s'\n", strings.Join(append(argsBase, argsFirstPass...), `" "`))
		fmt.Printf("second pass:\n\t'ffmpeg \"%s'\n", strings.Join(append(argsBase, argsSecondPass...), `" "`))
	}

	// it's off our paws now; pass execution and output to ffmpeg.
	ffmpegFirstPass := exec.CommandContext(ctx, "ffmpeg", append(argsBase, argsFirstPass...)...) // bet ya don't see this done often
	ffmpegFirstPass.Stdout = os.Stdout
	ffmpegFirstPass.Stderr = os.Stderr
	ffmpegFirstPass.Stdin = os.Stdin
	if err := ffmpegFirstPass.Run(); err != nil {
		os.Exit(1)
	}

	ffmpegSecondPass := exec.CommandContext(ctx, "ffmpeg", append(argsBase, argsSecondPass...)...) // ditto
	ffmpegSecondPass.Stdout = os.Stdout
	ffmpegSecondPass.Stderr = os.Stderr
	ffmpegSecondPass.Stdin = os.Stdin

	if err := ffmpegSecondPass.Run(); err != nil {
		os.Exit(1)
	}

}
