package main

import (
	"github.com/alonsovidales/pit/accounts_manager"
	"github.com/alonsovidales/pit/api"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/users"
	"github.com/alonsovidales/pit/shards_manager"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	if len(os.Args) > 1 {
		cfg.Init("pit", os.Args[1])

		log.SetLogger(
			log.Levels[cfg.GetStr("logger", "level")],
			cfg.GetStr("logger", "log_file"),
			cfg.GetInt("logger", "max_log_size_mb"),
		)
	} else {
		cfg.Init("pit", "dev")
	}
	runtime.GOMAXPROCS(runtime.NumCPU())

	usersModel := users.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	accountsManager := accountsmanager.Init(
		cfg.GetStr("rec-api", "base-url"),
		cfg.GetStr("mail", "addr"),
		cfg.GetStr("mail", "server"),
		cfg.GetInt("mail", "port"),
		usersModel)

	shardsManager := shardsmanager.Init(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		cfg.GetStr("aws", "s3-backups-path"),
		int(cfg.GetInt("rec-api", "port")),
		usersModel)

	api.Init(
		shardsManager,
		accountsManager,
		cfg.GetStr("rec-api", "static"))

	log.Info("System started...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGTERM)
	// Block until a signal is received.
	<-c

	log.Info("Stopping all the services")
	shardsManager.Stop()
}
