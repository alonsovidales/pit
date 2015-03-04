package main

import (
	"github.com/alonsovidales/pit/api"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/recommender"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	cfg.Init("pit", "dev")

	runtime.GOMAXPROCS(runtime.NumCPU())

	rc := recommender.Init(
		cfg.GetInt("shard", "memory_limit_mb"))

	api.Init(
		cfg.GetInt("server", "port"),
		rc)

	log.Info("System started...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGTERM)
	// Block until a signal is received.
	<-c
}
