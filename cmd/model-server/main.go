package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type Result struct {
	Label        string  `json:"label"`
	Score        float64 `json:"score"`
	ModelVersion string  `json:"modelVersion"`
}

type Predictor interface {
	Predict(ctx context.Context, text string) (Result, error)
}

type keywordPredictor struct {
	version  string
	keywords []string
}

// loadKeywordModel 读取模型文件：
// 以 "# version:" 开头的行是版本号，其他以 # 开头的行和空行跳过，剩下每行是一个关键词。
func loadKeywordModel(path string) (*keywordPredictor, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // 只读文件，Close 出错不影响已读到的内容，故意忽略

	p := &keywordPredictor{version: "unknown"}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if after, ok := strings.CutPrefix(line, "# version:"); ok {
			p.version = strings.TrimSpace(after)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p.keywords = append(p.keywords, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(p.keywords) == 0 {
		return nil, errors.New("model has no keywords")
	}
	return p, nil
}

func (p *keywordPredictor) Predict(_ context.Context, text string) (Result, error) {
	for _, keyword := range p.keywords {
		if strings.Contains(text, keyword) {
			return Result{
				Label:        "reject",
				Score:        1.0,
				ModelVersion: p.version,
			}, nil
		}
	}
	return Result{
		Label:        "approve",
		Score:        0.0,
		ModelVersion: p.version,
	}, nil
}

func predictHandler(p Predictor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text string `json:"text"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil || req.Text == "" {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		res, err := p.Predict(r.Context(), req.Text)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// 响应头已经发出，没法再改成 500，只能记一条日志（通常是客户端断开了）
		if err := json.NewEncoder(w).Encode(res); err != nil {
			slog.Warn("write response failed", "err", err)
		}
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	modelPath := getenv("MODEL_PATH", "/models/model.txt")
	port := getenv("PORT", "8080")

	p, err := loadKeywordModel(modelPath)
	if err != nil {
		slog.Error("load model failed", "path", modelPath, "err", err)
		os.Exit(1) // 加载失败直接退出，让 K8s 看到 CrashLoopBackOff
	}
	slog.Info("model loaded", "version", p.version, "keywords", len(p.keywords))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /predict", predictHandler(p)) // Go 1.22+ 支持在路由里写方法
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	// 优雅退出：收到 SIGTERM（K8s 删除 Pod 时发送）或 Ctrl+C 后，
	// 停止接收新请求，等正在处理的请求完成后再退出
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	go func() {
		slog.Info("listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown failed", "err", err)
	}
}
