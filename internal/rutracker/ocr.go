package rutracker

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var defaultOCR helperSolver

// WarmOCR loads the ddddocr helper beside this executable. Password login is the only caller.
func WarmOCR() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		defaultOCR.mu.Lock()
		defer defaultOCR.mu.Unlock()
		if err := defaultOCR.ensure(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "rutracker-mcp: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "rutracker-mcp: 验证码识别已就绪\n")
	}()
}

type helperSolver struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

type ocrReply struct {
	Text  string `json:"text"`
	Error string `json:"error"`
}

type readResult struct {
	line string
	err  error
}

func (h *helperSolver) Solve(ctx context.Context, image []byte) (string, error) {
	if len(image) == 0 || len(image) > maxCaptchaImage {
		return "", fmt.Errorf("%w: 验证码图片大小异常", ErrCaptcha)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.ensure(ctx); err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Image string `json:"image"`
	}{Image: base64.StdEncoding.EncodeToString(image)})
	if err != nil {
		return "", err
	}
	if _, err := h.in.Write(append(payload, '\n')); err != nil {
		h.stop()
		return "", fmt.Errorf("%w: 识别进程已退出", ErrCaptcha)
	}
	line, err := h.readLine(ctx)
	if err != nil {
		h.stop()
		return "", err
	}
	var reply ocrReply
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		h.stop()
		return "", fmt.Errorf("%w: 识别进程返回了无法解析的结果", ErrCaptcha)
	}
	if reply.Error != "" {
		return "", fmt.Errorf("%w: %s", ErrCaptcha, reply.Error)
	}
	if strings.TrimSpace(reply.Text) == "" {
		return "", fmt.Errorf("%w: 识别结果为空", ErrCaptcha)
	}
	return reply.Text, nil
}

func (h *helperSolver) ensure(ctx context.Context) error {
	if h.cmd != nil && h.in != nil && h.out != nil {
		return nil
	}
	h.stop()
	path, err := helperPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: 没有找到识别程序 %s", ErrCaptcha, path)
	}
	cmd := exec.Command(path)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: 无法启动识别程序: %v", ErrCaptcha, err)
	}
	h.cmd = cmd
	h.in = stdin
	h.out = bufio.NewReader(stdout)
	line, err := h.readLine(ctx)
	if err != nil {
		h.stop()
		return err
	}
	if !strings.Contains(line, `"ready"`) {
		h.stop()
		return fmt.Errorf("%w: 识别程序没有就绪", ErrCaptcha)
	}
	return nil
}

func (h *helperSolver) readLine(ctx context.Context) (string, error) {
	wait, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	reader := h.out
	done := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		done <- readResult{line: line, err: err}
	}()
	select {
	case <-wait.Done():
		h.stop()
		<-done
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: 识别超时", ErrCaptcha)
	case result := <-done:
		if result.err != nil {
			return "", fmt.Errorf("%w: 识别进程中断", ErrCaptcha)
		}
		return strings.TrimSpace(result.line), nil
	}
}

func (h *helperSolver) stop() {
	if h.in != nil {
		_ = h.in.Close()
		h.in = nil
	}
	if h.cmd != nil {
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		_ = h.cmd.Wait()
		h.cmd = nil
	}
	h.out = nil
}

func helperPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("RUTRACKER_OCR")); path != "" {
		return path, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrCaptcha, err.Error())
	}
	name := "rutracker-ocr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name), nil
}
