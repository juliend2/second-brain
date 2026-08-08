package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/joho/godotenv"

	"desrosiers.org/brain/internal/enrich"
	"desrosiers.org/brain/internal/store"
)

func main() {
	godotenv.Load()

	s, err := store.Open(env("BRAIN_DB", "data/brain.db"), env("BRAIN_NOTES", "data/notes"))
	if err != nil {
		fail(err.Error())
	}
	defer s.Close()

	e := enrich.New(s,
		enrich.NewOllamaEmbedder(env("OLLAMA_URL", "http://localhost:11434"), env("OLLAMA_EMBED_MODEL", "nomic-embed-text")),
		enrich.NewOpenAILLM(env("LLM_API_KEY", ""), env("LLM_BASE_URL", "https://api.openai.com/v1"), env("LLM_MODEL", "gpt-4o-mini")),
	)
	e.SetLLMWorkers(intEnv("ENRICH_WORKERS", 2))
	e.SetEmbedWorkers(intEnv("ENRICH_EMBED_WORKERS", 4))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "embed":
		stats, err := e.EmbedAll(ctx)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println("embed:", stats)
	case "cloud", "enrich":
		stats, err := e.EnrichAll(ctx)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println("cloud:", stats)
	case "all":
		stats, err := e.EmbedAll(ctx)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println("embed:", stats)
		stats, err = e.EnrichAll(ctx)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println("cloud:", stats)
	default:
		fail("usage: enrich [embed|cloud|all]")
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
