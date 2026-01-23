package system

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// HardwareAccelType represents available hardware acceleration types
type HardwareAccelType string

const (
	HW_NONE         HardwareAccelType = "none"
	HW_NVENC        HardwareAccelType = "nvenc"        // NVIDIA
	HW_QSV          HardwareAccelType = "qsv"          // Intel Quick Sync Video
	HW_AMF          HardwareAccelType = "amf"          // AMD Advanced Media Framework
	HW_VAAPI        HardwareAccelType = "vaapi"        // Video Acceleration API (Linux)
	HW_VIDEOTOOLBOX HardwareAccelType = "videotoolbox" // macOS
)

// HardwareCapabilities describes available hardware encoding capabilities
type HardwareCapabilities struct {
	AccelType   HardwareAccelType
	H264Encoder string   // FFmpeg encoder name for H.264
	HEVCEncoder string   // FFmpeg encoder name for HEVC/H.265
	DeviceArgs  []string // Additional FFmpeg args for device selection
}

var (
	detectedHardware   *HardwareCapabilities
	hardwareDetectOnce sync.Once
)

// DetectHardwareAcceleration detects available hardware acceleration
// Results are cached after first call
func DetectHardwareAcceleration() HardwareCapabilities {
	hardwareDetectOnce.Do(func() {
		detectedHardware = detectHardware()
	})
	return *detectedHardware
}

// ResetHardwareDetection clears the cached detection (useful for testing)
func ResetHardwareDetection() {
	hardwareDetectOnce = sync.Once{}
	detectedHardware = nil
}

func detectHardware() *HardwareCapabilities {
	// Check platform-specific acceleration first
	switch runtime.GOOS {
	case "darwin":
		if checkVideoToolbox() {
			return &HardwareCapabilities{
				AccelType:   HW_VIDEOTOOLBOX,
				H264Encoder: "h264_videotoolbox",
				HEVCEncoder: "hevc_videotoolbox",
			}
		}
	case "linux":
		// Check NVIDIA first (most common for encoding)
		if checkNVIDIA() {
			return &HardwareCapabilities{
				AccelType:   HW_NVENC,
				H264Encoder: "h264_nvenc",
				HEVCEncoder: "hevc_nvenc",
			}
		}
		// Check Intel QSV
		if checkQSV() {
			return &HardwareCapabilities{
				AccelType:   HW_QSV,
				H264Encoder: "h264_qsv",
				HEVCEncoder: "hevc_qsv",
			}
		}
		// Check AMD AMF
		if checkAMF() {
			return &HardwareCapabilities{
				AccelType:   HW_AMF,
				H264Encoder: "h264_amf",
				HEVCEncoder: "hevc_amf",
			}
		}
		// Check VAAPI (generic Linux)
		if checkVAAPI() {
			return &HardwareCapabilities{
				AccelType:   HW_VAAPI,
				H264Encoder: "h264_vaapi",
				HEVCEncoder: "hevc_vaapi",
				DeviceArgs:  []string{"-vaapi_device", "/dev/dri/renderD128"},
			}
		}
	case "windows":
		// Check NVIDIA
		if checkNVIDIA() {
			return &HardwareCapabilities{
				AccelType:   HW_NVENC,
				H264Encoder: "h264_nvenc",
				HEVCEncoder: "hevc_nvenc",
			}
		}
		// Check Intel QSV
		if checkQSV() {
			return &HardwareCapabilities{
				AccelType:   HW_QSV,
				H264Encoder: "h264_qsv",
				HEVCEncoder: "hevc_qsv",
			}
		}
		// Check AMD AMF
		if checkAMF() {
			return &HardwareCapabilities{
				AccelType:   HW_AMF,
				H264Encoder: "h264_amf",
				HEVCEncoder: "hevc_amf",
			}
		}
	}

	// Fallback to software encoding
	return &HardwareCapabilities{
		AccelType:   HW_NONE,
		H264Encoder: "libx264",
		HEVCEncoder: "libx265",
	}
}

// checkNVIDIA checks for NVIDIA GPU and NVENC support
func checkNVIDIA() bool {
	// Check if nvidia-smi exists and works
	cmd := exec.Command("nvidia-smi", "-L")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	if !strings.Contains(string(output), "GPU") {
		return false
	}

	// Verify FFmpeg has nvenc encoder
	return checkFFmpegEncoder("h264_nvenc")
}

// checkQSV checks for Intel Quick Sync Video support
func checkQSV() bool {
	// Check for Intel GPU on Linux
	if runtime.GOOS == "linux" {
		cmd := exec.Command("ls", "/dev/dri/")
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		if !strings.Contains(string(output), "renderD") {
			return false
		}
	}

	// Verify FFmpeg has QSV encoder
	return checkFFmpegEncoder("h264_qsv")
}

// checkAMF checks for AMD AMF support
func checkAMF() bool {
	// Verify FFmpeg has AMF encoder
	return checkFFmpegEncoder("h264_amf")
}

// checkVAAPI checks for VAAPI support
func checkVAAPI() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check if VAAPI device exists
	cmd := exec.Command("ls", "/dev/dri/renderD128")
	if err := cmd.Run(); err != nil {
		return false
	}

	// Verify FFmpeg has VAAPI encoder
	return checkFFmpegEncoder("h264_vaapi")
}

// checkVideoToolbox checks for macOS VideoToolbox support
func checkVideoToolbox() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return checkFFmpegEncoder("h264_videotoolbox")
}

// checkFFmpegEncoder verifies that FFmpeg has a specific encoder available
func checkFFmpegEncoder(encoder string) bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), encoder)
}

// GetHardwareAccelArgs returns FFmpeg arguments for hardware-accelerated encoding
// Returns empty slice if no hardware acceleration is available
func GetHardwareAccelArgs(hw HardwareCapabilities, codec string) []string {
	if hw.AccelType == HW_NONE {
		return nil
	}

	var args []string

	// Add device args if needed (e.g., VAAPI)
	args = append(args, hw.DeviceArgs...)

	// Add hardware acceleration input flag for some types
	switch hw.AccelType {
	case HW_NVENC:
		args = append(args, "-hwaccel", "cuda")
	case HW_QSV:
		args = append(args, "-hwaccel", "qsv")
	case HW_VAAPI:
		args = append(args, "-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi")
	case HW_VIDEOTOOLBOX:
		args = append(args, "-hwaccel", "videotoolbox")
	}

	return args
}
