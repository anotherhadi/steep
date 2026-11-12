// Command steep is a terminal UI for fuzzing HTTP requests with ffuf.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	fconfig "github.com/anotherhadi/foam/config"
	"github.com/anotherhadi/foam/theme"

	"github.com/anotherhadi/steep/internal/config"
	"github.com/anotherhadi/steep/internal/curl"
	"github.com/anotherhadi/steep/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "steep:", err)
		os.Exit(1)
	}
}

func run() error {
	args, sub, subArgs := config.SplitCommand(os.Args[1:])
	cfg, err := config.Load(args)
	if errors.Is(err, fconfig.ErrExit) {
		return nil
	}
	if err != nil {
		return err
	}
	var imported string
	if sub == "curl" {
		if imported, err = curl.ImportArgs(subArgs); err != nil {
			return fmt.Errorf("curl: %w", err)
		}
	}
	theme.Set(cfg.Theme)
	// ffuf logs through the standard logger: keep it off the screen.
	log.SetOutput(io.Discard)

	m, err := ui.New(cfg)
	if err != nil {
		return err
	}
	if imported != "" {
		m.Import(imported)
	}
	defer m.Close()

	p := tea.NewProgram(m)
	// ffuf's engine listens to SIGINT and SIGTERM to stop a run, and never
	// lets go of them: they lose their default behaviour for good after the
	// first run. steep quits on them itself; a second one kills it, in case
	// the first could not get through.
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigs
		go p.Quit()
		<-sigs
		p.Kill()
	}()
	_, err = p.Run()
	return err
}
