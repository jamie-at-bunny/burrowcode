package system

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FFmpegProgress represents the current progress of an FFmpeg operation
type FFmpegProgress struct {
	Frame       int64         // Current frame number
	FPS         float64       // Current frames per second
	Bitrate     string        // Current bitrate (e.g., "1234.5kbits/s")
	TotalSize   int64         // Output file size in bytes
	OutTimeUS   int64         // Output time in microseconds
	OutTime     time.Duration // Output time as duration
	DupFrames   int64         // Number of duplicate frames
	DropFrames  int64         // Number of dropped frames
	Speed       string        // Processing speed (e.g., "2.5x")
	Progress    string        // "continue" or "end"
	PercentDone float64       // Estimated percentage complete (0-100)
}

// ProgressCallback is called with progress updates during FFmpeg execution
type ProgressCallback func(progress FFmpegProgress)

// FFmpegRunner executes FFmpeg commands with progress tracking
type FFmpegRunner struct {
	DurationMS int64            // Total duration of input in milliseconds (for percentage calculation)
	OnProgress ProgressCallback // Called with progress updates
}

// Run executes an FFmpeg command with progress tracking
// The args should NOT include -progress as it will be added automatically
func (r *FFmpegRunner) Run(ctx context.Context, args []string) ([]byte, error) {
	// Create a pipe for progress output
	progressReader, progressWriter := io.Pipe()

	// Add progress flag to args
	fullArgs := append([]string{"-y", "-progress", "pipe:1"}, args...)

	cmd := exec.CommandContext(ctx, "ffmpeg", fullArgs...)
	cmd.Stdout = progressWriter

	// Capture stderr for error messages
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Read progress in a goroutine
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		r.parseProgress(progressReader)
	}()

	// Read stderr
	stderrOutput, _ := io.ReadAll(stderrPipe)

	// Wait for command to complete
	err = cmd.Wait()
	progressWriter.Close()
	<-progressDone

	return stderrOutput, err
}

// parseProgress reads FFmpeg progress output and calls the callback
func (r *FFmpegRunner) parseProgress(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	current := FFmpegProgress{}

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "frame":
			current.Frame, _ = strconv.ParseInt(value, 10, 64)
		case "fps":
			current.FPS, _ = strconv.ParseFloat(value, 64)
		case "bitrate":
			current.Bitrate = value
		case "total_size":
			current.TotalSize, _ = strconv.ParseInt(value, 10, 64)
		case "out_time_us":
			current.OutTimeUS, _ = strconv.ParseInt(value, 10, 64)
			current.OutTime = time.Duration(current.OutTimeUS) * time.Microsecond
		case "dup_frames":
			current.DupFrames, _ = strconv.ParseInt(value, 10, 64)
		case "drop_frames":
			current.DropFrames, _ = strconv.ParseInt(value, 10, 64)
		case "speed":
			current.Speed = value
		case "progress":
			current.Progress = value

			// Calculate percentage if we have duration info
			if r.DurationMS > 0 && current.OutTimeUS > 0 {
				current.PercentDone = float64(current.OutTimeUS/1000) / float64(r.DurationMS) * 100
				if current.PercentDone > 100 {
					current.PercentDone = 100
				}
			}

			// Call the callback
			if r.OnProgress != nil {
				r.OnProgress(current)
			}

			// Reset for next progress block (but keep calculating)
			if value == "end" {
				current.PercentDone = 100
			}
		}
	}
}

// GetMediaDuration returns the duration of a media file in milliseconds
func GetMediaDuration(path string) (int64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse duration (in seconds with decimal)
	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, err
	}

	return int64(duration * 1000), nil
}
