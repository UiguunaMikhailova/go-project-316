// Package main — консольная точка входа hexlet-go-crawler.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"code/crawler"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := newApp()

	if err := app.RunContext(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
}

func newApp() *cli.App {
	return &cli.App{
		Name:      "hexlet-go-crawler",
		Usage:     "analyze a website structure",
		ArgsUsage: "<url>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "depth",
				Usage: "crawl depth",
				Value: crawler.DefaultDepth,
			},
			&cli.IntFlag{
				Name:  "retries",
				Usage: "number of retries for failed requests",
				Value: crawler.DefaultRetries,
			},
			&cli.DurationFlag{
				Name:  "delay",
				Usage: "delay between requests (example: 200ms, 1s)",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "per-request timeout",
				Value: crawler.DefaultTimeout,
			},
			&cli.Float64Flag{
				Name:  "rps",
				Usage: "limit requests per second (overrides delay)",
			},
			&cli.StringFlag{
				Name:  "user-agent",
				Usage: "custom user agent",
			},
			&cli.IntFlag{
				Name:  "workers",
				Usage: "number of concurrent workers",
				Value: crawler.DefaultConcurrency,
			},
		},
		Action: run,
	}
}

func run(ctx *cli.Context) error {
	if ctx.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "error: url is required, example: hexlet-go-crawler https://example.com")

		return cli.ShowAppHelp(ctx)
	}

	report, err := crawler.Analyze(ctx.Context, optionsFrom(ctx))
	if err != nil {
		return err
	}

	fmt.Println(string(report))

	return nil
}

func optionsFrom(ctx *cli.Context) crawler.Options {
	return crawler.Options{
		URL:         ctx.Args().First(),
		Depth:       ctx.Int("depth"),
		Retries:     ctx.Int("retries"),
		Delay:       delayFrom(ctx),
		Timeout:     ctx.Duration("timeout"),
		UserAgent:   ctx.String("user-agent"),
		Concurrency: ctx.Int("workers"),
		IndentJSON:  true,
		HTTPClient:  &http.Client{},
	}
}

func delayFrom(ctx *cli.Context) time.Duration {
	if rps := ctx.Float64("rps"); rps > 0 {
		return time.Duration(float64(time.Second) / rps)
	}

	return ctx.Duration("delay")
}
