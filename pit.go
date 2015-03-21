package main

import (
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/shards_manager"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	cfg.Init("pit", "dev")

	runtime.GOMAXPROCS(runtime.NumCPU())

	manager := shardsmanager.Init(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		cfg.GetStr("aws", "s3-backups-path"),
		int(cfg.GetInt("rec-api", "port")))

	log.Info("System started...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGTERM)
	// Block until a signal is received.
	<-c

	log.Info("Stopping all the services")
	manager.Stop()
}
