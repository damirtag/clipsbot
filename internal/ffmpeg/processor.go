package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"mellclipsbot/internal/domain"
)

// Processor shells out to ffmpeg/ffprobe. It implements domain.VideoProcessor,
// so the rest of the app depends only on that interface — this is the piece
// that gets swapped out or extended for chroma key / blurred background /
// AI reframing later, without touching import or upload logic.
//
// Watermarking uses a pre-rendered PNG (see watermark.go) composited via
// ffmpeg's `overlay` filter, rather than the `drawtext` filter. drawtext
// requires ffmpeg to be built with libfreetype, which many ffmpeg builds
// (including Homebrew's default formula, and most slim Linux/Docker
// packages) don't include — that build gap is what "No such filter:
// 'drawtext'" means. `overlay` has no such dependency.
type Processor struct {
	ffmpegPath  string
	ffprobePath string
	version     int
	tempDir     string

	watermarkText string
	watermarkPath string
}

func NewProcessor(ffmpegPath string, version int, tempDir string) *Processor {
	ffprobePath := "ffprobe"
	if ffmpegPath != "ffmpeg" && ffmpegPath != "" {
		// best-effort: assume ffprobe sits alongside a custom ffmpeg path
		ffprobePath = ffmpegPath + "probe"
	}
	return &Processor{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath, version: version, tempDir: tempDir}
}

func (p *Processor) Version() int { return p.version }

// Process converts the input to a square, watermarked video sized for
// video-note-style delivery (square, capped at 640x640 per Telegram's
// video note convention)
func (p *Processor) Process(ctx context.Context, in domain.ProcessInput) (*domain.ProcessResult, error) {
	watermark := in.WatermarkText
	if watermark == "" {
		watermark = "@mellclipsbot"
	}

	watermarkPath, err := p.ensureWatermarkPNG(watermark)
	if err != nil {
		return nil, fmt.Errorf("render watermark: %w", err)
	}

	// Filter graph, two inputs (main video + watermark PNG):
	//  [0:v] crop to a centered square using the shorter dimension, then
	//        scale to 640x640 (Telegram's video note convention)      -> [bg]
	//  [bg][1:v] overlay the watermark bottom-center, 20px margin from the
	//        bottom edge so it clears typical face framing. If your source
	//        framing varies a lot, that margin is the first thing to tune.
	filterComplex := "[0:v]crop='min(in_w,in_h)':'min(in_w,in_h)',scale=640:640[bg];" +
		"[bg][1:v]overlay=(main_w-overlay_w)/2:main_h-overlay_h-20"

	args := []string{
		"-y",
		"-i", in.InputPath,
		"-i", watermarkPath,
		"-filter_complex", filterComplex,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "96k",
		"-movflags", "+faststart",
		in.OutputPath,
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg process failed: %w (stderr: %s)", err, stderr.String())
	}

	meta, err := p.probe(ctx, in.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("probe output: %w", err)
	}

	return &domain.ProcessResult{
		OutputPath: in.OutputPath,
		Duration:   meta.duration,
		Width:      meta.width,
		Height:     meta.height,
	}, nil
}

type probeResult struct {
	duration int
	width    int
	height   int
}

type ffprobeOutput struct {
	Streams []struct {
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p *Processor) probe(ctx context.Context, path string) (*probeResult, error) {
	args := []string{
		"-v", "error",
		"-show_entries", "stream=width,height,codec_type:format=duration",
		"-of", "json",
		path,
	}
	cmd := exec.CommandContext(ctx, p.ffprobePath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, stderr.String())
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	res := &probeResult{}
	for _, s := range parsed.Streams {
		if s.CodecType == "video" {
			res.width = s.Width
			res.height = s.Height
			break
		}
	}
	if parsed.Format.Duration != "" {
		if secs, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			res.duration = int(secs + 0.5)
		}
	}
	return res, nil
}

// ensureWatermarkPNG renders the watermark PNG once and reuses it across
// calls as long as the text hasn't changed, rather than re-rendering it
// for every clip.
func (p *Processor) ensureWatermarkPNG(text string) (string, error) {
	if p.watermarkPath != "" && p.watermarkText == text {
		if _, err := os.Stat(p.watermarkPath); err == nil {
			return p.watermarkPath, nil
		}
	}

	if err := os.MkdirAll(p.tempDir, 0o755); err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	path := filepath.Join(p.tempDir, "watermark.png")
	if err := generateWatermarkPNG(text, path); err != nil {
		return "", err
	}

	p.watermarkText = text
	p.watermarkPath = path
	return path, nil
}
