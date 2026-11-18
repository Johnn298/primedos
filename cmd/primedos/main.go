package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"primedos/internal/config"
	"primedos/internal/remote"
	"primedos/internal/ui"
)

func main() {
	cfg, err := config.Load(preConfigPath(os.Args[1:]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	flag.String("config", "", "путь к yaml-конфигу (читается до разбора флагов)")
	flag.StringVar(&cfg.Target, "target", cfg.Target, "цель (домен или URL)")
	flag.IntVar(&cfg.Level, "level", cfg.Level, "уровень атаки 1..3")
	flag.IntVar(&cfg.Mode, "mode", cfg.Mode, "режим 1..3 (до падения / бесконечно / фиксировано)")
	flag.StringVar(&cfg.Duration, "duration", cfg.Duration, "длительность для режима 3 (30s, 5m, 1h)")
	flag.IntVar(&cfg.Concurrency, "workers", cfg.Concurrency, "число воркеров")
	flag.Float64Var(&cfg.RateLimitMbps, "rate", cfg.RateLimitMbps, "лимит Мбит/с, 0 — без лимита")
	flag.StringVar(&cfg.ProxiesFile, "proxies", cfg.ProxiesFile, "файл со списком прокси")
	flag.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "интерфейс и телеметрия без генерации трафика")
	flag.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "подробный лог")
	flag.BoolVar(&cfg.HWTimestamps, "hw-ts", cfg.HWTimestamps, "аппаратные метки времени")
	socketPath := flag.String("socket", "", "unix-сокет для управления (JSON)")
	headless := flag.Bool("headless", false, "без TUI, движок под управлением сокета (только с --dry-run)")
	noTS := flag.Bool("no-ts", false, "отключить аппаратные метки времени")
	flag.Parse()

	if *noTS {
		cfg.HWTimestamps = false
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "конфиг:", err)
		os.Exit(2)
	}
	if *headless {
		if *socketPath == "" {
			fmt.Fprintln(os.Stderr, "--headless требует --socket")
			os.Exit(2)
		}
		if !cfg.DryRun {
			fmt.Fprintln(os.Stderr, "--headless работает только с --dry-run: удалённый запуск боевых сессий запрещён")
			os.Exit(2)
		}
	}

	fmt.Println("запускается ддос программа созданная тг @PrimeNikee")
	time.Sleep(400 * time.Millisecond)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *socketPath != "" {
		srv := remote.NewServer(*socketPath, cfg)
		go func() {
			if err := srv.Serve(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "сокет:", err)
				stop()
			}
		}()
	}

	if *headless {
		fmt.Println("headless: управление через сокет", *socketPath)
		<-ctx.Done()
		return
	}

	p := tea.NewProgram(ui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI:", err)
		os.Exit(1)
	}
}

func preConfigPath(args []string) string {
	for i, a := range args {
		if a == "-config" || a == "--config" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		for _, p := range []string{"-config=", "--config="} {
			if strings.HasPrefix(a, p) {
				return strings.TrimPrefix(a, p)
			}
		}
	}
	return ""
}
