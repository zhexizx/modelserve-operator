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
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
		}
	}(f)

	p := &keywordPredictor{version: "unknown"}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// TODO 1：按上面注释的规则处理每一行
		// 提示：strings.HasPrefix、strings.TrimPrefix
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
	// TODO 2：命中任意关键词返回 reject / 1.0，否则返回 approve / 0.0
	// 提示：strings.Contains；别忘了填 ModelVersion
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
		// TODO 3：用 json.NewDecoder(r.Body).Decode(&req) 解析请求体，
		//         失败或 text 为空时用 http.Error 返回 400
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
		json.NewEncoder(w).Encode(res)
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
	// TODO 4：创建一个 10 秒超时的 context，调用 srv.Shutdown(它)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown failed", "err", err)
	}
}
